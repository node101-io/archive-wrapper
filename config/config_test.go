package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/stretchr/testify/require"
)

func TestLoadDoesNotRequireRangeInWrapperConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
block_height_database_key: "archive-wrapper"
db_path: "./data/archive-wrapper"
grpc_listen_address: " 127.0.0.1:9090 "
control_socket_path: "/tmp/archive-wrapper.sock"
deployment_metadata_key: "archive-wrapper:deployment"
deployment_metadata:
  schema_version: 1
  mina_network_id: "testnet"
`), 0o644))

	got, err := Load(configPath)
	require.NoError(t, err)
	require.Equal(t, "archive-wrapper", got.BlockHeightDatabaseKey)
	require.Equal(t, "./data/archive-wrapper", got.DBPath)
	require.Equal(t, "127.0.0.1:9090", got.GRPCListenAddress)
	require.Equal(t, "/tmp/archive-wrapper.sock", got.ControlSocketPath)
	require.Equal(t, "archive-wrapper:deployment", got.DeploymentMetadataKey)
	require.Equal(t, uint32(1), got.DeploymentMetadata.SchemaVersion)
	require.Equal(t, "testnet", got.DeploymentMetadata.MinaNetworkID)
}

func TestConfigValidateGRPCListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr error
	}{
		{name: "ipv4_loopback", address: "127.0.0.1:9095"},
		{name: "ipv4_loopback_range", address: "127.20.30.40:9095"},
		{name: "ipv6_loopback", address: "[::1]:9095"},
		{name: "empty", wantErr: apperrors.ErrGRPCAddressRequired},
		{name: "surrounding_whitespace", address: " 127.0.0.1:9095 ", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "ipv4_unspecified", address: "0.0.0.0:9095", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "ipv6_unspecified", address: "[::]:9095", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "lan_ip", address: "192.168.1.10:9095", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "public_ip", address: "8.8.8.8:9095", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "hostname", address: "localhost:9095", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "missing_host", address: ":9095", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "missing_port", address: "127.0.0.1", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "zero_port", address: "127.0.0.1:0", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "port_too_large", address: "127.0.0.1:65536", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "service_name_port", address: "127.0.0.1:http", wantErr: apperrors.ErrInvalidGRPCListenAddress},
		{name: "zoned_ipv6", address: "[::1%lo]:9095", wantErr: apperrors.ErrInvalidGRPCListenAddress},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTestConfig()
			cfg.GRPCListenAddress = tt.address

			err := cfg.Validate()
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestLoadRejectsNonLoopbackGRPCListenAddress(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0:9095",
		"192.168.1.10:9095",
		"localhost:9095",
	} {
		t.Run(address, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			contents := fmt.Sprintf(`
block_height_database_key: "archive-wrapper"
db_path: "./data/archive-wrapper"
grpc_listen_address: %q
control_socket_path: "/tmp/archive-wrapper.sock"
deployment_metadata_key: "archive-wrapper:deployment"
deployment_metadata:
  schema_version: 1
  mina_network_id: "testnet"
`, address)
			require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o644))

			_, err := Load(configPath)
			require.ErrorIs(t, err, apperrors.ErrInvalidGRPCListenAddress)
		})
	}
}

func TestLoadRequiresMinaNetworkID(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
block_height_database_key: "archive-wrapper"
db_path: "./data/archive-wrapper"
grpc_listen_address: "127.0.0.1:9090"
control_socket_path: "/tmp/archive-wrapper.sock"
deployment_metadata_key: "archive-wrapper:deployment"
deployment_metadata:
  schema_version: 1
`), 0o644))

	_, err := Load(configPath)
	require.ErrorIs(t, err, apperrors.ErrMinaNetworkIDRequired)
}

func TestLoadRejectsConflictingDeploymentMetadataKey(t *testing.T) {
	for _, metadataKey := range []string{
		"archive-wrapper",
		"metadata",
	} {
		t.Run(metadataKey, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			contents := fmt.Sprintf(`
block_height_database_key: "archive-wrapper"
db_path: "./data/archive-wrapper"
grpc_listen_address: "127.0.0.1:9090"
control_socket_path: "/tmp/archive-wrapper.sock"
deployment_metadata_key: %q
deployment_metadata:
  schema_version: 1
  mina_network_id: "testnet"
`, metadataKey)
			require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o644))

			_, err := Load(configPath)
			require.ErrorIs(t, err, apperrors.ErrDeploymentMetadataKeyConflict)
		})
	}
}

func validTestConfig() Config {
	return Config{
		BlockHeightDatabaseKey: "archive-wrapper",
		DBPath:                 "./data/archive-wrapper",
		GRPCListenAddress:      "127.0.0.1:9095",
		ControlSocketPath:      "/tmp/archive-wrapper.sock",
		DeploymentMetadataKey:  "archive-wrapper:deployment",
		DeploymentMetadata: database.DeploymentMetadata{
			SchemaVersion: 1,
			MinaNetworkID: "testnet",
		},
	}
}
