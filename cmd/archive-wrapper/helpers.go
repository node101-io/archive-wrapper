package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/syndtr/goleveldb/leveldb"
)

// BridgeParams holds the x/bridge module params the wrapper consumes at startup.
type bridgeParams struct {
	ConfirmationDepth int64
	ContractAddress   string
	StartBlockHeight  int64
	MaxBlockRange     int64
}

type genesisFile struct {
	AppState struct {
		Bridge struct {
			Params struct {
				ConfirmationDepth int64  `json:"confirmation_depth,string"`
				ContractAddress   string `json:"contract_address"`
				StartBlockHeight  int64  `json:"start_block_height,string"`
				MaxBlockRange     int64  `json:"max_block_range,string"`
			} `json:"params"`
		} `json:"bridge"`
	} `json:"app_state"`
}

func loadGenesisFromHome(homePath string) (*genesisFile, error) {
	homePath = strings.TrimSpace(homePath)
	if homePath == "" {
		return nil, fmt.Errorf("chain home path is required")
	}

	content, err := os.ReadFile(filepath.Join(homePath, "config", "genesis.json"))
	if err != nil {
		return nil, fmt.Errorf("read genesis: %w", err)
	}

	var genesis genesisFile
	if err := json.Unmarshal(content, &genesis); err != nil {
		return nil, fmt.Errorf("unmarshal genesis: %w", err)
	}

	return &genesis, nil
}

// LoadBridgeParamsFromHome reads bridge params from the Pulsar genesis file.
func loadBridgeParamsFromHome(homePath string) (bridgeParams, error) {
	genesis, err := loadGenesisFromHome(homePath)
	if err != nil {
		return bridgeParams{}, err
	}

	contractAddress := strings.TrimSpace(genesis.AppState.Bridge.Params.ContractAddress)
	if contractAddress == "" {
		return bridgeParams{}, apperrors.ErrContractAddressRequired
	}
	if genesis.AppState.Bridge.Params.ConfirmationDepth <= 0 {
		return bridgeParams{}, apperrors.ErrConfirmationDepthRequired
	}
	if genesis.AppState.Bridge.Params.StartBlockHeight <= 0 {
		return bridgeParams{}, apperrors.ErrStartBlockHeightRequired
	}
	if genesis.AppState.Bridge.Params.MaxBlockRange <= 0 {
		return bridgeParams{}, apperrors.ErrMaxBlockRangeRequired
	}

	return bridgeParams{
		ConfirmationDepth: genesis.AppState.Bridge.Params.ConfirmationDepth,
		ContractAddress:   contractAddress,
		StartBlockHeight:  genesis.AppState.Bridge.Params.StartBlockHeight,
		MaxBlockRange:     genesis.AppState.Bridge.Params.MaxBlockRange,
	}, nil
}

// LoadLatestProcessedBlockHeight reads the persisted latest processed height cursor from LevelDB.
func loadLatestProcessedBlockHeight(
	dbPath string,
	blockHeightDatabaseKey string,
	logger *slog.Logger,
) (height int64, retErr error) {
	if logger == nil {
		return 0, apperrors.ErrNilLogger
	}

	db, err := database.NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := db.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close db: %w", err))
		}
	}()

	height, err = db.GetBlockHeight()
	if errors.Is(err, leveldb.ErrNotFound) {
		return 0, err
	}
	if err != nil {
		return 0, err
	}

	return height, nil
}

func ensureDBPathDoesNotExist(dbPath string) error {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return apperrors.ErrDBPathRequired
	}

	_, err := os.Stat(dbPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat db path: %w", err)
	}

	return fmt.Errorf("%w: %s (use 'make proceed' to resume)", apperrors.ErrDBAlreadyExists, dbPath)
}
