package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadDoesNotRequireRangeInWrapperConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
block_height_database_key: "archive-wrapper"
db_path: "./data/archive-wrapper"
grpc_listen_address: "127.0.0.1:9090"
control_socket_path: "/tmp/archive-wrapper.sock"
`), 0o644))

	got, err := Load(configPath)
	require.NoError(t, err)
	require.Equal(t, "archive-wrapper", got.BlockHeightDatabaseKey)
	require.Equal(t, "./data/archive-wrapper", got.DBPath)
	require.Equal(t, "127.0.0.1:9090", got.GRPCListenAddress)
	require.Equal(t, "/tmp/archive-wrapper.sock", got.ControlSocketPath)
}
