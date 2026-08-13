package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/config"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/stretchr/testify/require"
)

func TestRunRuntimeRejectsMissingPostgresBeforeSideEffects(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	dbPath := filepath.Join(root, "db")
	controlSocketPath := filepath.Join(root, "control.sock")
	inputs := testRuntimeInputs(dbPath, controlSocketPath)
	inputs.PostgresURI = ""

	err := runRuntime(context.Background(), inputs, func() {}, logger)
	require.ErrorIs(t, err, apperrors.ErrPostgresURIRequired)
	_, err = os.Stat(dbPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(controlSocketPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRunRuntimeRejectsDeploymentMismatchBeforePostgres(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := filepath.Join(t.TempDir(), "db")

	db, err := database.NewDbManager(dbPath, "db-key", logger)
	require.NoError(t, err)
	state, err := db.InitializeOrValidateDeployment("metadata-key", database.DeploymentMetadata{
		SchemaVersion:   1,
		MinaNetworkID:   "testnet",
		ContractAddress: "contract-a",
		StartHeight:     10,
	})
	require.NoError(t, err)
	require.Equal(t, database.DeploymentStateFresh, state)
	require.NoError(t, db.Close())
	err = runRuntime(
		context.Background(),
		runtimeInputs{
			Config: config.Config{
				BlockHeightDatabaseKey: "db-key",
				DBPath:                 dbPath,
				GRPCListenAddress:      "127.0.0.1:9095",
				ControlSocketPath:      filepath.Join(t.TempDir(), "control.sock"),
				DeploymentMetadataKey:  "metadata-key",
				DeploymentMetadata: database.DeploymentMetadata{
					SchemaVersion: 1,
					MinaNetworkID: "testnet",
				},
			},
			BridgeParams: bridgeParams{
				ContractAddress:  "contract-b",
				StartBlockHeight: 10,
			},
			PostgresURI: "postgres://postgres:secret@127.0.0.1/archive",
		},
		func() {},
		logger,
	)
	require.ErrorIs(t, err, apperrors.ErrDeploymentMetadataMismatch)
}

func TestRunRuntimeRejectsNonLoopbackGRPCAddressBeforeSideEffects(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	dbPath := filepath.Join(root, "db")
	controlSocketPath := filepath.Join(root, "control.sock")
	err := runRuntime(
		context.Background(),
		runtimeInputs{
			Config: config.Config{
				BlockHeightDatabaseKey: "db-key",
				DBPath:                 dbPath,
				GRPCListenAddress:      "0.0.0.0:9095",
				ControlSocketPath:      controlSocketPath,
				DeploymentMetadataKey:  "metadata-key",
				DeploymentMetadata: database.DeploymentMetadata{
					SchemaVersion: 1,
					MinaNetworkID: "testnet",
				},
			},
			BridgeParams: bridgeParams{
				ContractAddress:  "contract-a",
				StartBlockHeight: 10,
			},
			PostgresURI: "postgres://postgres:secret@127.0.0.1/archive",
		},
		func() {},
		logger,
	)
	require.ErrorIs(t, err, apperrors.ErrInvalidGRPCListenAddress)

	_, err = os.Stat(dbPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(controlSocketPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRunRuntimeInitializesFreshDatabaseBeforeRuntimeResources(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	dbPath := filepath.Join(root, "db")
	inputs := testRuntimeInputs(dbPath, filepath.Join(root, "missing", "control.sock"))

	err := runRuntime(context.Background(), inputs, func() {}, logger)
	require.Error(t, err)

	db, openErr := database.NewDbManager(dbPath, inputs.Config.BlockHeightDatabaseKey, logger)
	require.NoError(t, openErr)
	defer func() { require.NoError(t, db.Close()) }()
	state, validateErr := db.InitializeOrValidateDeployment(
		inputs.Config.DeploymentMetadataKey,
		expectedRuntimeMetadata(inputs),
	)
	require.NoError(t, validateErr)
	require.Equal(t, database.DeploymentStateInitialized, state)
	hasCursor, cursorErr := db.HasBlockHeight()
	require.NoError(t, cursorErr)
	require.False(t, hasCursor)
}

func TestRunRuntimePreservesInitializedCursor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	dbPath := filepath.Join(root, "db")
	inputs := testRuntimeInputs(dbPath, filepath.Join(root, "missing", "control.sock"))

	db, err := database.NewDbManager(dbPath, inputs.Config.BlockHeightDatabaseKey, logger)
	require.NoError(t, err)
	state, err := db.InitializeOrValidateDeployment(inputs.Config.DeploymentMetadataKey, expectedRuntimeMetadata(inputs))
	require.NoError(t, err)
	require.Equal(t, database.DeploymentStateFresh, state)
	require.NoError(t, db.InsertBlockHeight(541307))
	require.NoError(t, db.Close())

	err = runRuntime(context.Background(), inputs, func() {}, logger)
	require.Error(t, err)

	db, err = database.NewDbManager(dbPath, inputs.Config.BlockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	height, err := db.GetBlockHeight()
	require.NoError(t, err)
	require.Equal(t, int64(541307), height)
}

func TestRunRuntimeStopsCleanlyOnExternalCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	dbPath := filepath.Join(root, "db")
	controlSocketPath := testSocketPath(t, "runtime")
	inputs := testRuntimeInputs(dbPath, controlSocketPath)
	inputs.Config.GRPCListenAddress = reserveLoopbackAddress(t)
	inputs.BridgeParams.ContractAddress = testBridgeContractAddress
	inputs.PostgresURI = "postgres://postgres:secret@127.0.0.1:1/archive?connect_timeout=1"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runRuntime(ctx, inputs, cancel, logger)
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(controlSocketPath)
		return err == nil
	}, time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after cancellation")
	}
	_, err := os.Stat(controlSocketPath)
	require.ErrorIs(t, err, os.ErrNotExist)

	db, err := database.NewDbManager(dbPath, inputs.Config.BlockHeightDatabaseKey, logger)
	require.NoError(t, err, "runtime must release the LevelDB lock")
	require.NoError(t, db.Close())
}

func TestStopGRPCServerCompletesGracefully(t *testing.T) {
	server := newFakeGRPCStopper(true)
	forced := stopGRPCServer(server, time.Second)
	require.False(t, forced)
	require.Equal(t, int32(0), server.stopCalls.Load())
}

func TestStopGRPCServerForcesStopAfterTimeout(t *testing.T) {
	server := newFakeGRPCStopper(false)
	startedAt := time.Now()
	forced := stopGRPCServer(server, 10*time.Millisecond)
	require.True(t, forced)
	require.GreaterOrEqual(t, time.Since(startedAt), 10*time.Millisecond)
	require.Equal(t, int32(1), server.stopCalls.Load())
}

func TestRuntimeWorkerErrors(t *testing.T) {
	fatalErr := errors.New("fatal worker error")
	require.ErrorIs(t, unexpectedWorkerExit(runtimeWorkerResult{name: "indexer", err: fatalErr}), fatalErr)
	require.ErrorContains(t, unexpectedWorkerExit(runtimeWorkerResult{name: "indexer"}), "stopped unexpectedly")
	require.NoError(t, shutdownWorkerError(runtimeWorkerResult{name: "indexer", err: context.Canceled}))
	require.ErrorIs(t, shutdownWorkerError(runtimeWorkerResult{name: "indexer", err: fatalErr}), fatalErr)
}

func testRuntimeInputs(dbPath, controlSocketPath string) runtimeInputs {
	return runtimeInputs{
		Config: config.Config{
			BlockHeightDatabaseKey: "db-key",
			DBPath:                 dbPath,
			GRPCListenAddress:      "127.0.0.1:9095",
			ControlSocketPath:      controlSocketPath,
			DeploymentMetadataKey:  "metadata-key",
			DeploymentMetadata: database.DeploymentMetadata{
				SchemaVersion: 1,
				MinaNetworkID: "testnet",
			},
		},
		BridgeParams: bridgeParams{
			ConfirmationDepth: 32,
			ContractAddress:   "contract-a",
			StartBlockHeight:  10,
			MaxBlockRange:     1000,
		},
		PostgresURI: "postgres://postgres:secret@127.0.0.1/archive",
	}
}

func expectedRuntimeMetadata(inputs runtimeInputs) database.DeploymentMetadata {
	metadata := inputs.Config.DeploymentMetadata
	metadata.ContractAddress = inputs.BridgeParams.ContractAddress
	metadata.StartHeight = inputs.BridgeParams.StartBlockHeight
	return metadata
}

func reserveLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}

type fakeGRPCStopper struct {
	release   chan struct{}
	stopCalls atomic.Int32
	once      sync.Once
}

func newFakeGRPCStopper(graceful bool) *fakeGRPCStopper {
	server := &fakeGRPCStopper{release: make(chan struct{})}
	if graceful {
		close(server.release)
	}
	return server
}

func (server *fakeGRPCStopper) GracefulStop() {
	<-server.release
}

func (server *fakeGRPCStopper) Stop() {
	server.stopCalls.Add(1)
	server.once.Do(func() { close(server.release) })
}
