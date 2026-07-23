package config

import (
	"fmt"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/spf13/viper"
)

type Config struct {
	ContractAddress        string `mapstructure:"contract_address"`
	BlockHeightDatabaseKey string `mapstructure:"block_height_database_key"`
	DBPath                 string `mapstructure:"db_path"`
	GRPCListenAddress      string `mapstructure:"grpc_listen_address"`
	ControlSocketPath      string `mapstructure:"control_socket_path"`
	MaxActionRangeHeights  int64  `mapstructure:"max_action_range_heights"`
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
		return Config{}, apperrors.ErrGrpcAddressRequired
	}
	if cfg.ContractAddress == "" {
		return Config{}, apperrors.ErrContractAddressRequired
	}
	if cfg.BlockHeightDatabaseKey == "" {
		return Config{}, apperrors.ErrBlockHeightDbKeyRequired
	}
	if cfg.DBPath == "" {
		return Config{}, apperrors.ErrDbPathRequired
	}
	if cfg.ControlSocketPath == "" {
		return Config{}, apperrors.ErrControlSocketPathRequired
	}
	if cfg.MaxActionRangeHeights <= 0 {
		return Config{}, apperrors.ErrMaxActionRangeRequired
	}

	return cfg, nil
}
