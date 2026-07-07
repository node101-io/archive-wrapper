package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	ContractAddress        string `mapstructure:"contract_address"`
	BlockHeightDatabaseKey string `mapstructure:"block_height_database_key"`
	DBPath                 string `mapstructure:"db_path"`
	ConfirmationDepth      int64  `mapstructure:"confirmation_depth"`
	GRPCListenAddress      string `mapstructure:"grpc_listen_address"`
	ControlSocketPath      string `mapstructure:"control_socket_path"`
}

func Load(configFile string) (Config, error) {
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
	cfg.GRPCListenAddress = strings.TrimSpace(cfg.GRPCListenAddress)
	cfg.ControlSocketPath = strings.TrimSpace(cfg.ControlSocketPath)

	if cfg.GRPCListenAddress == "" {
		return Config{}, fmt.Errorf("grpc_listen_address is required")
	}
	if cfg.ContractAddress == "" {
		return Config{}, fmt.Errorf("contract_address is required")
	}
	if cfg.BlockHeightDatabaseKey == "" {
		return Config{}, fmt.Errorf("block_height_database_key is required")
	}
	if cfg.DBPath == "" {
		return Config{}, fmt.Errorf("db_path is required")
	}
	if cfg.ControlSocketPath == "" {
		return Config{}, fmt.Errorf("control_socket_path is required")
	}
	if cfg.ConfirmationDepth <= 0 {
		return Config{}, fmt.Errorf("confirmation_depth is required and must be greater than 0")
	}

	return cfg, nil
}
