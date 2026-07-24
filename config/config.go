package config

import (
	"fmt"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/spf13/viper"
)

// Config holds the runtime settings loaded from the wrapper config file.
type Config struct {
	// ContractAddress is the Mina zkApp address whose actions are indexed.
	ContractAddress string `mapstructure:"contract_address"`
	// BlockHeightDatabaseKey stores the latest processed block cursor key.
	BlockHeightDatabaseKey string `mapstructure:"block_height_database_key"`
	// DBPath is the local LevelDB path used for indexed data.
	DBPath string `mapstructure:"db_path"`
	// GRPCListenAddress is the address where the query server listens.
	GRPCListenAddress string `mapstructure:"grpc_listen_address"`
	// ControlSocketPath is the unix socket path used by start and stop commands.
	ControlSocketPath string `mapstructure:"control_socket_path"`
	// MaxActionRangeHeights limits each query to this many inclusive block heights.
	MaxActionRangeHeights int64 `mapstructure:"max_action_range_heights"`
}

// Load reads configFile, normalizes string fields, and validates required values.
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
		return Config{}, apperrors.ErrGRPCAddressRequired
	}
	if cfg.ContractAddress == "" {
		return Config{}, apperrors.ErrContractAddressRequired
	}
	if cfg.BlockHeightDatabaseKey == "" {
		return Config{}, apperrors.ErrBlockHeightDBKeyRequired
	}
	if cfg.DBPath == "" {
		return Config{}, apperrors.ErrDBPathRequired
	}
	if cfg.ControlSocketPath == "" {
		return Config{}, apperrors.ErrControlSocketPathRequired
	}
	if cfg.MaxActionRangeHeights <= 0 {
		return Config{}, apperrors.ErrMaxActionRangeRequired
	}

	return cfg, nil
}
