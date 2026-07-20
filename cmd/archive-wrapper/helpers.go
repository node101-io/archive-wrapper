package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/spf13/viper"
)

func LoadConfirmationDepthFromHome(homePath string) (int64, error) {
	homePath = strings.TrimSpace(homePath)
	if homePath == "" {
		return 0, fmt.Errorf("chain home path is required")
	}

	v := viper.New()
	v.SetConfigFile(filepath.Join(homePath, "config", "app.toml"))

	if err := v.ReadInConfig(); err != nil {
		return 0, fmt.Errorf("read app config: %w", err)
	}

	confirmationDepth := v.GetInt64("confirmation_depth")
	if confirmationDepth <= 0 {
		return 0, apperrors.ErrConfirmationDepthRequired
	}

	return confirmationDepth, nil
}
