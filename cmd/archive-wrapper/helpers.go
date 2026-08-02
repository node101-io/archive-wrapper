package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
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

// TODO: Currently we only read from genesis.json. Values are static.
// Wrapper should be able to update these values since Pulsar consensus may decide to change values as well.
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
