package config

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/spf13/viper"
)

// Config holds the runtime settings loaded from the wrapper config file.
type Config struct {
	// BlockHeightDatabaseKey stores the latest processed block cursor key.
	BlockHeightDatabaseKey string `mapstructure:"block_height_database_key"`
	// DBPath is the local LevelDB path used for indexed data.
	DBPath string `mapstructure:"db_path"`
	// GRPCListenAddress is the address where the query server listens.
	GRPCListenAddress string `mapstructure:"grpc_listen_address"`
	// ControlSocketPath is the unix socket path used by start and stop commands.
	ControlSocketPath string `mapstructure:"control_socket_path"`
	// DeploymentMetadataKey stores deployment identity in LevelDB.
	DeploymentMetadataKey string `mapstructure:"deployment_metadata_key"`
	// DeploymentMetadata identifies the history stored in the configured database.
	DeploymentMetadata database.DeploymentMetadata `mapstructure:"deployment_metadata"`
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

	cfg.BlockHeightDatabaseKey = strings.TrimSpace(cfg.BlockHeightDatabaseKey)
	cfg.DBPath = strings.TrimSpace(cfg.DBPath)
	cfg.GRPCListenAddress = strings.TrimSpace(cfg.GRPCListenAddress)
	cfg.ControlSocketPath = strings.TrimSpace(cfg.ControlSocketPath)
	cfg.DeploymentMetadataKey = strings.TrimSpace(cfg.DeploymentMetadataKey)
	cfg.DeploymentMetadata.MinaNetworkID = strings.TrimSpace(cfg.DeploymentMetadata.MinaNetworkID)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate checks the static settings required before starting the wrapper.
func (cfg Config) Validate() error {
	if cfg.GRPCListenAddress == "" {
		return apperrors.ErrGRPCAddressRequired
	}
	if err := validateGRPCListenAddress(cfg.GRPCListenAddress); err != nil {
		return err
	}
	if cfg.BlockHeightDatabaseKey == "" {
		return apperrors.ErrBlockHeightDBKeyRequired
	}
	if cfg.DBPath == "" {
		return apperrors.ErrDBPathRequired
	}
	if cfg.ControlSocketPath == "" {
		return apperrors.ErrControlSocketPathRequired
	}
	if cfg.DeploymentMetadataKey == "" {
		return apperrors.ErrDeploymentMetadataKeyRequired
	}
	// Metadata must not overlap cursor or 8-byte block-record keys.
	if cfg.DeploymentMetadataKey == cfg.BlockHeightDatabaseKey ||
		len(cfg.DeploymentMetadataKey) == 8 {
		return apperrors.ErrDeploymentMetadataKeyConflict
	}
	if cfg.DeploymentMetadata.SchemaVersion == 0 {
		return apperrors.ErrDeploymentSchemaVersionRequired
	}
	if cfg.DeploymentMetadata.MinaNetworkID == "" {
		return apperrors.ErrMinaNetworkIDRequired
	}

	return nil
}

func validateGRPCListenAddress(address string) error {
	if address != strings.TrimSpace(address) {
		return fmt.Errorf("%w: surrounding whitespace is not allowed", apperrors.ErrInvalidGRPCListenAddress)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidGRPCListenAddress, err)
	}

	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: host must be a literal IP address", apperrors.ErrInvalidGRPCListenAddress)
	}
	if ip.Zone() != "" {
		return fmt.Errorf("%w: scoped IPv6 addresses are not allowed", apperrors.ErrInvalidGRPCListenAddress)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("%w: IP address is not loopback", apperrors.ErrInvalidGRPCListenAddress)
	}

	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return fmt.Errorf("%w: port must be between 1 and 65535", apperrors.ErrInvalidGRPCListenAddress)
	}

	return nil
}
