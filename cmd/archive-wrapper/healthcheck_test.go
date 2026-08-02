package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	grpcHealth "google.golang.org/grpc/health"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
)

func TestHealthDialAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
		wantErr bool
	}{
		{name: "ipv4", address: "127.0.0.1:9095", want: "127.0.0.1:9095"},
		{name: "ipv4_wildcard", address: "0.0.0.0:9095", want: "127.0.0.1:9095"},
		{name: "ipv6", address: "[::1]:9095", want: "[::1]:9095"},
		{name: "ipv6_wildcard", address: "[::]:9095", want: "[::1]:9095"},
		{name: "hostname", address: "localhost:9095", wantErr: true},
		{name: "zero_port", address: "127.0.0.1:0", wantErr: true},
		{name: "empty", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := healthDialAddress(tt.address)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveHealthcheckAddressPrecedence(t *testing.T) {
	configPath := writeHealthcheckConfig(t, "127.0.0.1:9001")
	lookup := mapLookup(map[string]string{
		"ARCHIVE_WRAPPER_CONFIG":              configPath,
		"ARCHIVE_WRAPPER_HEALTHCHECK_ADDRESS": "127.0.0.1:9002",
	})

	got, err := resolveHealthcheckAddress("127.0.0.1:9003", true, "", false, lookup)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9003", got)

	got, err = resolveHealthcheckAddress("", false, "", false, lookup)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9002", got)

	got, err = resolveHealthcheckAddress("", false, configPath, true, mapLookup(nil))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9001", got)
}

func TestResolveHealthcheckAddressUsesRuntimeEnvironment(t *testing.T) {
	configPath := writeHealthcheckConfig(t, "127.0.0.1:9001")
	t.Setenv("ARCHIVE_WRAPPER_GRPC_LISTEN_ADDRESS", "0.0.0.0:9004")
	t.Setenv("ARCHIVE_WRAPPER_GRPC_TRANSPORT_MODE", "trusted-network")

	got, err := resolveHealthcheckAddress("", false, configPath, true, os.LookupEnv)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9004", got)
}

func TestResolveHealthcheckAddressRejectsEmptyExplicitValues(t *testing.T) {
	_, err := resolveHealthcheckAddress("", true, "unused", true, mapLookup(nil))
	require.ErrorIs(t, err, apperrors.ErrGRPCAddressRequired)

	_, err = resolveHealthcheckAddress("", false, "", true, mapLookup(nil))
	require.ErrorIs(t, err, apperrors.ErrConfigPathRequired)

	_, err = resolveHealthcheckAddress("", false, "", false, mapLookup(map[string]string{
		"ARCHIVE_WRAPPER_HEALTHCHECK_ADDRESS": "",
	}))
	require.ErrorIs(t, err, apperrors.ErrGRPCAddressRequired)
}

func TestCheckQueryHealth(t *testing.T) {
	tests := []struct {
		name    string
		status  grpcHealthV1.HealthCheckResponse_ServingStatus
		wantErr bool
	}{
		{name: "serving", status: grpcHealthV1.HealthCheckResponse_SERVING},
		{name: "not_serving", status: grpcHealthV1.HealthCheckResponse_NOT_SERVING, wantErr: true},
		{name: "unknown", status: grpcHealthV1.HealthCheckResponse_UNKNOWN, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			address, stop := serveTestHealth(t, tt.status)
			defer stop()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := checkQueryHealth(ctx, address)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCheckQueryHealthFailsForUnavailableTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, checkQueryHealth(ctx, "127.0.0.1:1"))
}

func TestRunMainHealthcheckDoesNotInitializeRuntimeLogger(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "healthcheck.log")
	t.Setenv("ARCHIVE_WRAPPER_LOG_PATH", logPath)
	var stderr bytes.Buffer

	exitCode := runMain([]string{"healthcheck", "--address", "127.0.0.1:1", "--timeout", "10ms"}, &stderr)
	require.Equal(t, exitCodeFailure, exitCode)
	_, err := os.Stat(logPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRunHealthcheckRejectsNonPositiveTimeout(t *testing.T) {
	err := runHealthcheckCommand([]string{"--address", "127.0.0.1:9095", "--timeout", "0s"}, mapLookup(nil))
	require.ErrorContains(t, err, "greater than zero")
}

func serveTestHealth(
	t *testing.T,
	status grpcHealthV1.HealthCheckResponse_ServingStatus,
) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	health := grpcHealth.NewServer()
	health.SetServingStatus(queryGRPCServiceName, status)
	grpcHealthV1.RegisterHealthServer(server, health)
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().String(), server.Stop
}

func writeHealthcheckConfig(t *testing.T, address string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
grpc_listen_address: "` + address + `"
grpc_transport_mode: "loopback"
`)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
