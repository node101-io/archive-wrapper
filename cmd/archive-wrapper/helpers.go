package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/spf13/viper"
)

func loadAppConfigFromHome(homePath string) (*viper.Viper, error) {
	homePath = strings.TrimSpace(homePath)
	if homePath == "" {
		return nil, fmt.Errorf("chain home path is required")
	}

	v := viper.New()
	v.SetConfigFile(filepath.Join(homePath, "config", "app.toml"))

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read app config: %w", err)
	}

	return v, nil
}

// LoadContractAddressFromHome reads bridge.contract_address from the chain home app.toml.
func LoadContractAddressFromHome(homePath string) (string, error) {
	v, err := loadAppConfigFromHome(homePath)
	if err != nil {
		return "", err
	}
	contractAddress := strings.TrimSpace(v.GetString("bridge.contract_address"))
	if contractAddress == "" {
		return "", apperrors.ErrContractAddressRequired
	}

	return contractAddress, nil
}

// LoadConfirmationDepthFromHome reads bridge.confirmation_depth from the chain home app.toml.
func LoadConfirmationDepthFromHome(homePath string) (int64, error) {
	v, err := loadAppConfigFromHome(homePath)
	if err != nil {
		return 0, err
	}

	confirmationDepth := v.GetInt64("bridge.confirmation_depth")
	if confirmationDepth <= 0 {
		return 0, apperrors.ErrConfirmationDepthRequired
	}

	return confirmationDepth, nil
}
