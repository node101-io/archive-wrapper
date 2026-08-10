package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/stretchr/testify/require"
)

func TestRunCommandRejectsMissingPostgresBeforeDatabaseCreation(t *testing.T) {
	clearCommandEnvironment(t)
	root := t.TempDir()
	dbPath := filepath.Join(root, "wrapper-db")
	controlSocketPath := filepath.Join(root, "control.sock")
	homePath := filepath.Join(root, "chain-home")
	writeGenesis(t, homePath, testGenesisJSON())
	configPath := writeRuntimeConfig(t, dbPath, controlSocketPath)
	t.Setenv("POSTGRES_URI", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := run(
		[]string{"run", "--config", configPath, "--home", homePath},
		ctx,
		cancel,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	require.ErrorIs(t, err, apperrors.ErrPostgresURIRequired)

	_, err = os.Stat(dbPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(controlSocketPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRemovedLifecycleCommandsAreRejected(t *testing.T) {
	clearCommandEnvironment(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, command := range []string{"start", "proceed"} {
		t.Run(command, func(t *testing.T) {
			err := run([]string{command}, context.Background(), func() {}, logger)
			require.ErrorContains(t, err, "unknown command")
		})
	}
}

func TestCommandsRejectUnexpectedArguments(t *testing.T) {
	clearCommandEnvironment(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name string
		args []string
	}{
		{name: "run", args: []string{"run", "unexpected"}},
		{name: "stop", args: []string{"stop", "unexpected"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.args, context.Background(), func() {}, logger)
			require.ErrorContains(t, err, "does not accept positional arguments")
		})
	}
}

func TestStopRejectsLegacySocketPathFlag(t *testing.T) {
	clearCommandEnvironment(t)
	err := run(
		[]string{"stop", "--socket-path", "/tmp/wrapper.sock"},
		context.Background(),
		func() {},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	require.ErrorContains(t, err, "flag provided but not defined")
}

func TestResolveConfigPathPrecedence(t *testing.T) {
	clearCommandEnvironment(t)
	tests := []struct {
		name    string
		args    []string
		env     *string
		want    string
		wantErr error
	}{
		{name: "cli_over_environment", args: []string{"--config", "cli.yaml"}, env: stringPointer("env.yaml"), want: "cli.yaml"},
		{name: "environment", env: stringPointer("env.yaml"), want: "env.yaml"},
		{name: "explicit_empty_cli", args: []string{"--config="}, env: stringPointer("env.yaml"), wantErr: apperrors.ErrConfigPathRequired},
		{name: "explicit_empty_environment", env: stringPointer(""), wantErr: apperrors.ErrConfigPathRequired},
		{name: "missing", wantErr: apperrors.ErrConfigPathRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnvironment(t, "ARCHIVE_WRAPPER_CONFIG")
			if tt.env != nil {
				t.Setenv("ARCHIVE_WRAPPER_CONFIG", *tt.env)
			}

			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			configPath := fs.String("config", "", "")
			require.NoError(t, fs.Parse(tt.args))

			got, err := resolveConfigPath(fs, *configPath)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveStopSocketPathPrecedence(t *testing.T) {
	clearCommandEnvironment(t)
	configPath := writeStopConfig(t, "/tmp/config.sock")

	t.Run("cli_over_environment_and_config", func(t *testing.T) {
		t.Setenv("ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH", "/tmp/env.sock")
		got, err := resolveStopSocketPath("/tmp/cli.sock", true, configPath, true)
		require.NoError(t, err)
		require.Equal(t, "/tmp/cli.sock", got)
	})

	t.Run("environment_over_config", func(t *testing.T) {
		t.Setenv("ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH", "/tmp/env.sock")
		got, err := resolveStopSocketPath("", false, configPath, true)
		require.NoError(t, err)
		require.Equal(t, "/tmp/env.sock", got)
	})

	t.Run("config", func(t *testing.T) {
		unsetEnvironment(t, "ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH")
		got, err := resolveStopSocketPath("", false, configPath, true)
		require.NoError(t, err)
		require.Equal(t, "/tmp/config.sock", got)
	})

	t.Run("explicit_empty_cli", func(t *testing.T) {
		_, err := resolveStopSocketPath("", true, configPath, true)
		require.ErrorIs(t, err, apperrors.ErrControlSocketPathRequired)
	})

	t.Run("explicit_empty_environment", func(t *testing.T) {
		t.Setenv("ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH", "")
		_, err := resolveStopSocketPath("", false, configPath, true)
		require.ErrorIs(t, err, apperrors.ErrControlSocketPathRequired)
	})
}

func writeStopConfig(t *testing.T, socketPath string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
block_height_database_key: "archive-wrapper"
db_path: "./data/archive-wrapper"
grpc_listen_address: "127.0.0.1:9095"
grpc_transport_mode: "loopback"
control_socket_path: "` + socketPath + `"
deployment_metadata_key: "archive-wrapper:deployment"
deployment_metadata:
  schema_version: 1
  mina_network_id: "testnet"
`)
	require.NoError(t, os.WriteFile(configPath, contents, 0o600))
	return configPath
}

func writeRuntimeConfig(t *testing.T, dbPath, socketPath string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
block_height_database_key: "archive-wrapper"
db_path: "` + dbPath + `"
grpc_listen_address: "127.0.0.1:9095"
grpc_transport_mode: "loopback"
control_socket_path: "` + socketPath + `"
deployment_metadata_key: "archive-wrapper:deployment"
deployment_metadata:
  schema_version: 1
  mina_network_id: "testnet"
`)
	require.NoError(t, os.WriteFile(configPath, contents, 0o600))
	return configPath
}

func testGenesisJSON() string {
	return `{
  "app_state": {
    "bridge": {
      "params": {
        "confirmation_depth": "32",
        "contract_address": "` + testBridgeContractAddress + `",
        "start_block_height": "537276",
        "max_block_range": "1000"
      }
    }
  }
}`
}

func unsetEnvironment(t *testing.T, key string) {
	t.Helper()
	oldValue, wasSet := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if wasSet {
			require.NoError(t, os.Setenv(key, oldValue))
			return
		}
		require.NoError(t, os.Unsetenv(key))
	})
}

func stringPointer(value string) *string {
	return &value
}

func clearCommandEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ARCHIVE_WRAPPER_CONFIG",
		"ARCHIVE_WRAPPER_CHAIN_HOME",
		"ARCHIVE_WRAPPER_DB_PATH",
		"ARCHIVE_WRAPPER_GRPC_LISTEN_ADDRESS",
		"ARCHIVE_WRAPPER_GRPC_TRANSPORT_MODE",
		"ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH",
		"ARCHIVE_WRAPPER_HEALTHCHECK_ADDRESS",
		"ARCHIVE_WRAPPER_LOG_PATH",
		"POSTGRES_URI",
	} {
		oldValue, wasSet := os.LookupEnv(key)
		require.NoError(t, os.Unsetenv(key))
		t.Cleanup(func() {
			if wasSet {
				require.NoError(t, os.Setenv(key, oldValue))
				return
			}
			require.NoError(t, os.Unsetenv(key))
		})
	}
}
