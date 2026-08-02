package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/stretchr/testify/require"
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

	got, err := loadBridgeParamsFromHome(homePath)
	require.NoError(t, err)
	require.Equal(t, bridgeParams{
		ConfirmationDepth: 32,
		ContractAddress:   testBridgeContractAddress,
		StartBlockHeight:  537276,
		MaxBlockRange:     1000,
	}, got)
}

func TestLoadBridgeParamsFromHomeRejectsMissingGenesis(t *testing.T) {
	homePath := t.TempDir()

	got, err := loadBridgeParamsFromHome(homePath)
	require.Equal(t, bridgeParams{}, got)
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

	got, err := loadBridgeParamsFromHome(homePath)
	require.Equal(t, bridgeParams{}, got)
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

	got, err := loadBridgeParamsFromHome(homePath)
	require.Equal(t, bridgeParams{}, got)
	require.ErrorIs(t, err, apperrors.ErrStartBlockHeightRequired)
}

func writeGenesis(t *testing.T, homePath, contents string) {
	t.Helper()

	configPath := filepath.Join(homePath, "config")
	require.NoError(t, os.MkdirAll(configPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configPath, "genesis.json"), []byte(contents), 0o644))
}
