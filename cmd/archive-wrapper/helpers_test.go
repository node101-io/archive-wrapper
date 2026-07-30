package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb"
)

const testBridgeContractAddress = "B62qjRDirGFRf5dvNcGzMs5oWzQ2VyNcygnoKM2MkxB9PFUp7Utdraf"

func TestLoadBridgeParamsFromHome(t *testing.T) {
	homePath := t.TempDir()
	writeGenesis(t, homePath, `{
  "app_state": {
    "bridge": {
      "params": {
        "confirmation_depth": "32",
        "contract_address": "`+testBridgeContractAddress+`",
        "start_block_height": "537276",
        "max_block_range": "1000"
      }
    }
  }
}`)

	got, err := LoadBridgeParamsFromHome(homePath)
	require.NoError(t, err)
	require.Equal(t, BridgeParams{
		ConfirmationDepth: 32,
		ContractAddress:   testBridgeContractAddress,
		StartBlockHeight:  537276,
		MaxBlockRange:     1000,
	}, got)
}

func TestLoadBridgeParamsFromHomeRejectsMissingGenesis(t *testing.T) {
	homePath := t.TempDir()

	got, err := LoadBridgeParamsFromHome(homePath)
	require.Equal(t, BridgeParams{}, got)
	require.ErrorContains(t, err, "read genesis")
}

func TestLoadBridgeParamsFromHomeRejectsInvalidMaxBlockRange(t *testing.T) {
	homePath := t.TempDir()
	writeGenesis(t, homePath, `{
  "app_state": {
    "bridge": {
      "params": {
        "confirmation_depth": "32",
        "contract_address": "`+testBridgeContractAddress+`",
        "start_block_height": "537276",
        "max_block_range": "0"
      }
    }
  }
}`)

	got, err := LoadBridgeParamsFromHome(homePath)
	require.Equal(t, BridgeParams{}, got)
	require.ErrorIs(t, err, apperrors.ErrMaxBlockRangeRequired)
}

func TestLoadBridgeParamsFromHomeRejectsInvalidStartBlockHeight(t *testing.T) {
	homePath := t.TempDir()
	writeGenesis(t, homePath, `{
  "app_state": {
    "bridge": {
      "params": {
        "confirmation_depth": "32",
        "contract_address": "`+testBridgeContractAddress+`",
        "start_block_height": "0",
        "max_block_range": "1000"
      }
    }
  }
}`)

	got, err := LoadBridgeParamsFromHome(homePath)
	require.Equal(t, BridgeParams{}, got)
	require.ErrorIs(t, err, apperrors.ErrStartBlockHeightRequired)
}

func TestLoadLatestProcessedBlockHeight(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := t.TempDir()

	manager, err := database.NewDbManager(dbPath, "db-key", logger)
	require.NoError(t, err)

	require.NoError(t, manager.InsertBlockHeight(541307))
	require.NoError(t, manager.Close())

	got, err := LoadLatestProcessedBlockHeight(dbPath, "db-key", logger)
	require.NoError(t, err)
	require.Equal(t, int64(541307), got)
}

func TestLoadLatestProcessedBlockHeightReturnsNotFoundWhenCursorMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := t.TempDir()

	manager, err := database.NewDbManager(dbPath, "db-key", logger)
	require.NoError(t, err)
	require.NoError(t, manager.Close())

	got, err := LoadLatestProcessedBlockHeight(dbPath, "db-key", logger)
	require.Zero(t, got)
	require.ErrorIs(t, err, leveldb.ErrNotFound)
}

func writeGenesis(t *testing.T, homePath, contents string) {
	t.Helper()

	configPath := filepath.Join(homePath, "config")
	require.NoError(t, os.MkdirAll(configPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configPath, "genesis.json"), []byte(contents), 0o644))
}
