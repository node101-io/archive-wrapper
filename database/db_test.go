package database

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"

	"github.com/stretchr/testify/require"
)

const blockHeightDatabaseKey = "db-key"

func TestDbManager_InsertThenGet(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)

	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	want := actions.DbRecord{
		Key: 7,
		Actions: []*actions.Action{
			{
				BlockHeight: 7,
				FeePayer:    []byte("alice"),
				ActionType:  actions.ActionType_DEPOSIT,
				Amount:      42,
			},
		},
	}

	err = manager.Insert(want)
	require.NoError(t, err)

	got, err := manager.Get(want.Key)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, got.Key, want.Key)
	require.Equal(t, len(got.Actions), 1) // Because we inserted only 1 action

	// Ensure they have the same values
	require.Equal(t, got.Actions[0].BlockHeight, want.Actions[0].BlockHeight)
	require.Equal(t, got.Actions[0].FeePayer, want.Actions[0].FeePayer)
	require.Equal(t, got.Actions[0].ActionType, want.Actions[0].ActionType)
	require.Equal(t, got.Actions[0].Amount, want.Actions[0].Amount)

}

func TestDbManagerInsertBlockHeight(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	var currentHeight int64 = 8

	err = manager.InsertBlockHeight(currentHeight)
	require.NoError(t, err)

	got, err := manager.GetBlockHeight()
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, got, currentHeight)

	currentHeight += 1
	err = manager.InsertBlockHeight(currentHeight)
	require.NoError(t, err)

	got, err = manager.GetBlockHeight()
	require.NoError(t, err)
	require.NotNil(t, got)

}
func TestDbManagerInsertBlockHeightRejectsRegression(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	require.NoError(t, manager.InsertBlockHeight(10))

	err = manager.InsertBlockHeight(9)
	require.Error(t, err)
	require.True(t, errors.Is(err, apperrors.ErrBlockHeightRegression))
}

func TestDbManagerEnsureDeploymentMetadataPersistsAndAcceptsMatch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := t.TempDir()
	metadata := testDeploymentMetadata()

	manager, err := NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NoError(t, manager.EnsureDeploymentMetadata("archive-wrapper:deployment", metadata))
	require.NoError(t, manager.Close())

	manager, err = NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, manager.Close())
	}()

	require.NoError(t, manager.EnsureDeploymentMetadata("archive-wrapper:deployment", metadata))
}

func TestDbManagerEnsureDeploymentMetadataRejectsMismatch(t *testing.T) {
	want := testDeploymentMetadata()
	tests := []struct {
		name   string
		mutate func(*DeploymentMetadata)
	}{
		{
			name: "schema version",
			mutate: func(metadata *DeploymentMetadata) {
				metadata.SchemaVersion++
			},
		},
		{
			name: "Mina network",
			mutate: func(metadata *DeploymentMetadata) {
				metadata.MinaNetworkID = "mainnet"
			},
		},
		{
			name: "contract address",
			mutate: func(metadata *DeploymentMetadata) {
				metadata.ContractAddress = "B62qDifferentContract"
			},
		},
		{
			name: "start height",
			mutate: func(metadata *DeploymentMetadata) {
				metadata.StartHeight++
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
			require.NoError(t, err)
			defer func() {
				require.NoError(t, manager.Close())
			}()

			require.NoError(t, manager.EnsureDeploymentMetadata("archive-wrapper:deployment", want))

			got := want
			tt.mutate(&got)
			err = manager.EnsureDeploymentMetadata("archive-wrapper:deployment", got)
			require.ErrorIs(t, err, apperrors.ErrDeploymentMetadataMismatch)
		})
	}
}

func TestDbManagerEnsureDeploymentMetadataRejectsIndexedDatabaseWithoutMetadata(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, manager.Close())
	}()

	require.NoError(t, manager.InsertBlockHeight(10))

	err = manager.EnsureDeploymentMetadata("archive-wrapper:deployment", testDeploymentMetadata())
	require.ErrorIs(t, err, apperrors.ErrDeploymentMetadataMissing)
}

func TestDbManagerEnsureDeploymentMetadataAllowsInitializedDatabaseWithoutCursor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := t.TempDir()
	manager, err := NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NoError(t, manager.EnsureDeploymentMetadata(
		"archive-wrapper:deployment",
		testDeploymentMetadata(),
	))
	require.NoError(t, manager.Close())

	manager, err = NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, manager.Close())
	}()

	require.NoError(t, manager.EnsureDeploymentMetadata(
		"archive-wrapper:deployment",
		testDeploymentMetadata(),
	))
	hasCursor, err := manager.HasBlockHeight()
	require.NoError(t, err)
	require.False(t, hasCursor)
}

func testDeploymentMetadata() DeploymentMetadata {
	return DeploymentMetadata{
		SchemaVersion:   1,
		MinaNetworkID:   "testnet",
		ContractAddress: "B62qContract",
		StartHeight:     10,
	}
}
