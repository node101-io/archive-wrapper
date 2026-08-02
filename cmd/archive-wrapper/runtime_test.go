package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/config"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/stretchr/testify/require"
)

func TestRunStartRejectsDeploymentMismatchBeforePostgres(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := filepath.Join(t.TempDir(), "db")

	db, err := database.NewDbManager(dbPath, "db-key", logger)
	require.NoError(t, err)
	require.NoError(t, db.EnsureDeploymentMetadata("metadata-key", database.DeploymentMetadata{
		SchemaVersion:   1,
		MinaNetworkID:   "testnet",
		ContractAddress: "contract-a",
		StartHeight:     10,
	}))
	require.NoError(t, db.Close())
	t.Setenv("POSTGRES_URI", "")

	err = runStart(
		context.Background(),
		config.Config{
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
		bridgeParams{
			ContractAddress:  "contract-b",
			StartBlockHeight: 10,
		},
		func() {},
		logger,
	)
	require.ErrorIs(t, err, apperrors.ErrDeploymentMetadataMismatch)
}

func TestRunStartRejectsNonLoopbackGRPCAddressBeforeSideEffects(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	dbPath := filepath.Join(root, "db")
	controlSocketPath := filepath.Join(root, "control.sock")
	t.Setenv("POSTGRES_URI", "")

	err := runStart(
		context.Background(),
		config.Config{
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
		bridgeParams{
			ContractAddress:  "contract-a",
			StartBlockHeight: 10,
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
