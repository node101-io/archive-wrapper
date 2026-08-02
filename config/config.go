package config

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/spf13/viper"
)

// TransportMode defines which network boundary may expose the plaintext gRPC server.
type TransportMode string

const (
	// TransportModeLoopback restricts the listener to literal loopback addresses.
	TransportModeLoopback TransportMode = "loopback"
	// TransportModeTrustedNetwork permits private and unspecified bind addresses.
	// TODO(security): trusted-network records explicit operator acceptance of
	// plaintext gRPC; it does not verify network isolation. Add TLS/mTLS before
	// supporting endpoints outside a controlled private deployment network.
	TransportModeTrustedNetwork TransportMode = "trusted-network"
)

// Config holds the runtime settings loaded from the wrapper config file.
type Config struct {
	// ChainHome is the Pulsar home containing the authoritative bridge genesis.
	ChainHome string `mapstructure:"chain_home"`
	// BlockHeightDatabaseKey stores the latest processed block cursor key.
	BlockHeightDatabaseKey string `mapstructure:"block_height_database_key"`
	// DBPath is the local LevelDB path used for indexed data.
	DBPath string `mapstructure:"db_path"`
	// GRPCListenAddress is the address where the query server listens.
	GRPCListenAddress string `mapstructure:"grpc_listen_address"`
	// GRPCTransportMode selects the bind policy for the plaintext gRPC server.
	GRPCTransportMode TransportMode `mapstructure:"grpc_transport_mode"`
	// ControlSocketPath is the unix socket path used by run and stop commands.
	ControlSocketPath string `mapstructure:"control_socket_path"`
	// DeploymentMetadataKey stores deployment identity in LevelDB.
	DeploymentMetadataKey string `mapstructure:"deployment_metadata_key"`
	// DeploymentMetadata identifies the history stored in the configured database.
	DeploymentMetadata database.DeploymentMetadata `mapstructure:"deployment_metadata"`
}

// Overrides contains command-line values that were explicitly provided.
// A nil pointer means the source did not override the config value.
type Overrides struct {
	ChainHome         *string
	GRPCListenAddress *string
	GRPCTransportMode *string
	DBPath            *string
	ControlSocketPath *string
}

// Load reads only configFile, applies defaults, normalizes fields, and validates it.
func Load(configFile string) (Config, error) {
	return resolve(configFile, Overrides{}, func(string) (string, bool) { return "", false }, false)
}

// Resolve loads a config file and applies defaults, environment values, and
// explicit CLI overrides in increasing precedence order. ChainHome is required
// in the resulting runtime configuration.
func Resolve(configFile string, overrides Overrides) (Config, error) {
	return resolve(configFile, overrides, os.LookupEnv, true)
}

// ResolveGRPCListenAddress resolves only the effective gRPC listener settings.
// Short-lived probes use this path without requiring chain or database inputs.
func ResolveGRPCListenAddress(configFile string) (string, error) {
	if strings.TrimSpace(configFile) == "" {
		return "", apperrors.ErrConfigPathRequired
	}

	cfg, err := decode(configFile)
	if err != nil {
		return "", err
	}
	applyDefaults(&cfg)
	applyEnvironment(&cfg, os.LookupEnv)
	normalize(&cfg)

	if cfg.GRPCListenAddress == "" {
		return "", apperrors.ErrGRPCAddressRequired
	}
	if err := validateGRPCListenAddress(cfg.GRPCListenAddress, cfg.EffectiveGRPCTransportMode()); err != nil {
		return "", err
	}
	return cfg.GRPCListenAddress, nil
}

type lookupEnv func(string) (string, bool)

func resolve(configFile string, overrides Overrides, lookup lookupEnv, requireChainHome bool) (Config, error) {
	if strings.TrimSpace(configFile) == "" {
		return Config{}, apperrors.ErrConfigPathRequired
	}

	cfg, err := decode(configFile)
	if err != nil {
		return Config{}, err
	}

	applyDefaults(&cfg)
	applyEnvironment(&cfg, lookup)
	applyOverrides(&cfg, overrides)
	normalize(&cfg)

	if requireChainHome && cfg.ChainHome == "" {
		return Config{}, apperrors.ErrChainHomeRequired
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func decode(configFile string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(configFile)
	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.GRPCTransportMode == "" {
		cfg.GRPCTransportMode = TransportModeLoopback
	}
}

func applyEnvironment(cfg *Config, lookup lookupEnv) {
	if value, ok := lookup("ARCHIVE_WRAPPER_CHAIN_HOME"); ok {
		cfg.ChainHome = value
	}
	if value, ok := lookup("ARCHIVE_WRAPPER_GRPC_LISTEN_ADDRESS"); ok {
		cfg.GRPCListenAddress = value
	}
	if value, ok := lookup("ARCHIVE_WRAPPER_GRPC_TRANSPORT_MODE"); ok {
		cfg.GRPCTransportMode = TransportMode(value)
	}
	if value, ok := lookup("ARCHIVE_WRAPPER_DB_PATH"); ok {
		cfg.DBPath = value
	}
	if value, ok := lookup("ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH"); ok {
		cfg.ControlSocketPath = value
	}
}

func applyOverrides(cfg *Config, overrides Overrides) {
	if overrides.ChainHome != nil {
		cfg.ChainHome = *overrides.ChainHome
	}
	if overrides.GRPCListenAddress != nil {
		cfg.GRPCListenAddress = *overrides.GRPCListenAddress
	}
	if overrides.GRPCTransportMode != nil {
		cfg.GRPCTransportMode = TransportMode(*overrides.GRPCTransportMode)
	}
	if overrides.DBPath != nil {
		cfg.DBPath = *overrides.DBPath
	}
	if overrides.ControlSocketPath != nil {
		cfg.ControlSocketPath = *overrides.ControlSocketPath
	}
}

func normalize(cfg *Config) {
	cfg.ChainHome = strings.TrimSpace(cfg.ChainHome)
	cfg.BlockHeightDatabaseKey = strings.TrimSpace(cfg.BlockHeightDatabaseKey)
	cfg.DBPath = strings.TrimSpace(cfg.DBPath)
	cfg.GRPCListenAddress = strings.TrimSpace(cfg.GRPCListenAddress)
	cfg.GRPCTransportMode = TransportMode(strings.TrimSpace(string(cfg.GRPCTransportMode)))
	cfg.ControlSocketPath = strings.TrimSpace(cfg.ControlSocketPath)
	cfg.DeploymentMetadataKey = strings.TrimSpace(cfg.DeploymentMetadataKey)
	cfg.DeploymentMetadata.MinaNetworkID = strings.TrimSpace(cfg.DeploymentMetadata.MinaNetworkID)
}

// EffectiveGRPCTransportMode keeps direct Config construction consistent with file loading.
func (cfg Config) EffectiveGRPCTransportMode() TransportMode {
	if cfg.GRPCTransportMode == "" {
		return TransportModeLoopback
	}
	return cfg.GRPCTransportMode
}

// Validate checks the static settings required before starting the wrapper.
func (cfg Config) Validate() error {
	if cfg.GRPCListenAddress == "" {
		return apperrors.ErrGRPCAddressRequired
	}
	if err := validateGRPCListenAddress(cfg.GRPCListenAddress, cfg.EffectiveGRPCTransportMode()); err != nil {
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

func validateGRPCListenAddress(address string, mode TransportMode) error {
	if address != strings.TrimSpace(address) {
		return fmt.Errorf("%w: surrounding whitespace is not allowed", apperrors.ErrInvalidGRPCListenAddress)
	}
	if mode != TransportModeLoopback && mode != TransportModeTrustedNetwork {
		return fmt.Errorf("%w: %q", apperrors.ErrUnsupportedGRPCTransportMode, mode)
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
	if mode == TransportModeLoopback && !ip.IsLoopback() {
		return fmt.Errorf("%w: IP address is not loopback", apperrors.ErrInvalidGRPCListenAddress)
	}
	if mode == TransportModeTrustedNetwork &&
		!ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() {
		return fmt.Errorf("%w: IP address is not loopback, private, or unspecified", apperrors.ErrInvalidGRPCListenAddress)
	}

	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return fmt.Errorf("%w: port must be between 1 and 65535", apperrors.ErrInvalidGRPCListenAddress)
	}

	return nil
}
