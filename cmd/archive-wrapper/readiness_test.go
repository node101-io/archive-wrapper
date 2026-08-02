package main

import (
	"context"
	"testing"

	"github.com/node101-io/archive-wrapper/diagnostics"
	"github.com/node101-io/archive-wrapper/indexer"
	"github.com/node101-io/archive-wrapper/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	grpcHealth "google.golang.org/grpc/health"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
)

func TestRegisterGRPCServicesStartsQueryUnavailableAndDiagnosticsServing(t *testing.T) {
	grpcServer := grpc.NewServer()
	store := diagnostics.NewStore()
	healthServer := registerGRPCServices(
		grpcServer,
		&query.UnimplementedQueryServer{},
		diagnostics.NewServer(store),
	)

	requireHealthStatus(t, healthServer, "", grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	requireHealthStatus(t, healthServer, queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	requireHealthStatus(t, healthServer, diagnosticsGRPCServiceName, grpcHealthV1.HealthCheckResponse_SERVING)
}

func TestReadinessControllerTransitionsThroughSyncAndReconnect(t *testing.T) {
	healthServer, store, controller := newTestReadinessController()

	controller.Connecting()
	require.Equal(t, diagnostics.OperationalState_OPERATIONAL_STATE_CONNECTING, store.Snapshot().State)
	requireHealthStatus(t, healthServer, queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_NOT_SERVING)

	controller.OnSyncStarted()
	controller.OnSyncProgress(indexer.SyncProgress{ArchiveHeight: 42, TargetHeight: 10})
	controller.OnSyncCompleted(indexer.SyncProgress{ArchiveHeight: 42, TargetHeight: 10})
	require.Equal(t, diagnostics.OperationalState_OPERATIONAL_STATE_WAITING_FOR_FINALITY, store.Snapshot().State)
	require.False(t, store.Snapshot().Ready)

	readyProgress := indexer.SyncProgress{
		ArchiveHeight: 45,
		TargetHeight:  13,
		Initialized:   true,
		IndexedHeight: 13,
	}
	controller.OnSyncStarted()
	controller.OnSyncCompleted(readyProgress)
	require.Equal(t, diagnostics.OperationalState_OPERATIONAL_STATE_READY, store.Snapshot().State)
	require.True(t, store.Snapshot().Ready)
	requireHealthStatus(t, healthServer, queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_SERVING)

	controller.OnSyncStarted()
	require.Equal(t, diagnostics.OperationalState_OPERATIONAL_STATE_SYNCING, store.Snapshot().State)
	require.True(t, store.Snapshot().Ready)
	requireHealthStatus(t, healthServer, queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_SERVING)

	controller.Reconnecting("postgres notification connection unavailable")
	require.Equal(t, diagnostics.OperationalState_OPERATIONAL_STATE_RECONNECTING, store.Snapshot().State)
	require.False(t, store.Snapshot().Ready)
	require.Equal(t, "postgres notification connection unavailable", store.Snapshot().LastError)
	requireHealthStatus(t, healthServer, queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_NOT_SERVING)
}

func TestReadinessControllerStoppingIsTerminal(t *testing.T) {
	healthServer, store, controller := newTestReadinessController()
	controller.Stopping()
	controller.OnSyncCompleted(indexer.SyncProgress{Initialized: true, IndexedHeight: 10})
	controller.Connecting()

	require.Equal(t, diagnostics.OperationalState_OPERATIONAL_STATE_STOPPING, store.Snapshot().State)
	require.False(t, store.Snapshot().Ready)
	requireHealthStatus(t, healthServer, queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_NOT_SERVING)
}

func newTestReadinessController() (*grpcHealth.Server, *diagnostics.Store, *readinessController) {
	healthServer := grpcHealth.NewServer()
	healthServer.SetServingStatus("", grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(diagnosticsGRPCServiceName, grpcHealthV1.HealthCheckResponse_SERVING)
	store := diagnostics.NewStore()
	return healthServer, store, newReadinessController(healthServer, store)
}

func requireHealthStatus(
	t *testing.T,
	healthServer *grpcHealth.Server,
	service string,
	expected grpcHealthV1.HealthCheckResponse_ServingStatus,
) {
	t.Helper()
	response, err := healthServer.Check(context.Background(), &grpcHealthV1.HealthCheckRequest{Service: service})
	require.NoError(t, err)
	require.Equal(t, expected, response.Status)
}
