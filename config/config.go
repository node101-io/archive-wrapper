package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

const configFile = "../config.yaml"

type Config struct {
	ContractAddress        string `mapstructure:"contract_address"`
	BlockHeightDatabaseKey string `mapstructure:"block_height_database_key"`
	DBPath                 string `mapstructure:"db_path"`
}

func Load() (Config, error) {

	v := viper.New()
	v.SetConfigFile(configFile)

	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.ContractAddress = strings.TrimSpace(cfg.ContractAddress)
	cfg.BlockHeightDatabaseKey = strings.TrimSpace(cfg.BlockHeightDatabaseKey)
	cfg.DBPath = strings.TrimSpace(cfg.DBPath)

	if cfg.ContractAddress == "" {
		return Config{}, fmt.Errorf("contract_address is required")
	}
	if cfg.BlockHeightDatabaseKey == "" {
		return Config{}, fmt.Errorf("block_height_database_key is required")
	}
	if cfg.DBPath == "" {
		return Config{}, fmt.Errorf("db_path is required")
	}

	return cfg, nil
}
