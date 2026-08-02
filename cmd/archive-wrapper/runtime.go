package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/config"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/node101-io/archive-wrapper/diagnostics"
	"github.com/node101-io/archive-wrapper/fetchmina"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/node101-io/archive-wrapper/query"
	"google.golang.org/grpc"
	grpcHealth "google.golang.org/grpc/health"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

type runtimeInputs struct {
	Config       config.Config
	BridgeParams bridgeParams
	PostgresURI  string
}

// Service names must match the generated descriptors used by gRPC health clients.
const queryGRPCServiceName = "query.Query"
const diagnosticsGRPCServiceName = "diagnostics.DiagnosticsService"

const grpcShutdownTimeout = 10 * time.Second

type grpcStopper interface {
	GracefulStop()
	Stop()
}

type runtimeWorkerResult struct {
	name string
	err  error
}

// registerGRPCServices exposes query, diagnostics, health, and reflection on one server.
func registerGRPCServices(
	grpcServer *grpc.Server,
	queryService query.QueryServer,
	diagnosticsService diagnostics.DiagnosticsServiceServer,
) *grpcHealth.Server {
	query.RegisterQueryServer(grpcServer, queryService)
	diagnostics.RegisterDiagnosticsServiceServer(grpcServer, diagnosticsService)

	healthServer := grpcHealth.NewServer()
	grpcHealthV1.RegisterHealthServer(grpcServer, healthServer)
	reflection.Register(grpcServer)
	// Query traffic stays unavailable until the initial reconciliation succeeds.
	healthServer.SetServingStatus("", grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(diagnosticsGRPCServiceName, grpcHealthV1.HealthCheckResponse_SERVING)

	return healthServer
}

// runRuntime builds the long-lived runtime and coordinates all component lifecycles.
func runRuntime(
	ctx context.Context,
	inputs runtimeInputs,
	cancel context.CancelFunc,
	logger *slog.Logger,
) (retErr error) {
	cfg := inputs.Config
	bridgeParams := inputs.BridgeParams
	if err := cfg.Validate(); err != nil {
		return err
	}
	postgresURI := strings.TrimSpace(inputs.PostgresURI)
	if postgresURI == "" {
		return apperrors.ErrPostgresURIRequired
	}

	runtimeLogger := logger.With("component", "runtime")
	// Complete deployment metadata with canonical genesis values.
	cfg.DeploymentMetadata.ContractAddress = bridgeParams.ContractAddress
	cfg.DeploymentMetadata.StartHeight = bridgeParams.StartBlockHeight

	runtimeLogger.Info(
		"starting archive wrapper",
		"start_block_height",
		bridgeParams.StartBlockHeight,
		"grpc_listen_address",
		cfg.GRPCListenAddress,
		"grpc_transport_mode",
		cfg.EffectiveGRPCTransportMode(),
		"control_socket_path",
		cfg.ControlSocketPath,
		"confirmation_depth",
		bridgeParams.ConfirmationDepth,
		"contract_address",
		bridgeParams.ContractAddress,
		"mina_network_id",
		cfg.DeploymentMetadata.MinaNetworkID,
		"max_block_range",
		bridgeParams.MaxBlockRange,
		"db_path",
		cfg.DBPath,
	)
	if cfg.EffectiveGRPCTransportMode() == config.TransportModeTrustedNetwork {
		runtimeLogger.Warn(
			"trusted-network gRPC transport enabled",
			"warning",
			"plaintext transport requires operator-controlled network isolation",
		)
	}

	db, err := database.NewDbManager(cfg.DBPath, cfg.BlockHeightDatabaseKey, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close db: %w", err))
		}
	}()

	// Reject a mismatched deployment before opening external network connections.
	deploymentState, err := db.InitializeOrValidateDeployment(cfg.DeploymentMetadataKey, cfg.DeploymentMetadata)
	if err != nil {
		return err
	}
	runtimeLogger.Info("deployment state validated", "state", deploymentState)

	control, err := listenControlSocket(cfg.ControlSocketPath, cancel, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := control.Close(); err != nil {
			retErr = errors.Join(retErr, err)
		}
		<-control.Done()
	}()

	runtimeLogger.Info("connecting to postgres")

	queryPool, err := pgxpool.New(ctx, postgresURI)
	if err != nil {
		return fmt.Errorf("%w: %w", apperrors.ErrPostgresConfigurationInvalid, err)
	}
	runtimeLogger.Info("postgres query pool created")
	defer queryPool.Close()

	client, err := fetchmina.NewMinaClient(
		bridgeParams.ContractAddress,
		sqlcdb.New(queryPool),
		logger,
	)
	if err != nil {
		return err
	}

	grpcListener, err := net.Listen("tcp", cfg.GRPCListenAddress)
	if err != nil {
		return fmt.Errorf("listen gRPC: %w", err)
	}
	defer func() {
		if err := grpcListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			retErr = errors.Join(retErr, fmt.Errorf("close gRPC listener: %w", err))
		}
	}()

	grpcServer := grpc.NewServer()
	queryService, err := query.NewQuery(
		db,
		logger,
		cfg.DeploymentMetadata.StartHeight,
		bridgeParams.MaxBlockRange,
	)
	if err != nil {
		return err
	}
	diagnosticsStore := diagnostics.NewStore()
	diagnosticsService := diagnostics.NewServer(diagnosticsStore)
	healthServer := registerGRPCServices(grpcServer, queryService, diagnosticsService)
	readiness := newReadinessController(healthServer, diagnosticsStore)

	// External cancellation wakes the coordinator; workers are canceled only
	// after readiness has been withdrawn.
	runCtx, cancelRuntime := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRuntime()
	workerResults := make(chan runtimeWorkerResult, 2)

	go func() {
		runtimeLogger.Info("starting indexer run loop")

		// Each retry creates a fresh notification connection and Indexer instance.
		session := func(sessionCtx context.Context) error {
			return runIndexerSession(
				sessionCtx,
				postgresURI,
				client,
				db,
				bridgeParams.StartBlockHeight,
				bridgeParams.ConfirmationDepth,
				postgresProbeTimeout,
				connectPostgresNotification,
				readiness,
				logger,
				runtimeLogger,
			)
		}
		err := superviseIndexer(runCtx, queryPool, session, readiness, reconnectPolicy{
			ProbeTimeout: postgresProbeTimeout,
			RetryDelay:   notificationReconnectDelay,
		}, runtimeLogger)
		workerResults <- runtimeWorkerResult{name: "indexer", err: err}
	}()

	go func() {
		runtimeLogger.Info("gRPC server listening", "address", cfg.GRPCListenAddress)

		err := grpcServer.Serve(grpcListener)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			runtimeLogger.Error("gRPC server stopped with error", "err", err)
		}
		workerResults <- runtimeWorkerResult{name: "gRPC", err: err}
	}()

	completedWorkers := 0
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			retErr = ctx.Err()
		}
	case result := <-workerResults:
		completedWorkers++
		retErr = unexpectedWorkerExit(result)
	}

	runtimeLogger.Info("shutdown requested, updating gRPC health status")
	readiness.Stopping()
	healthServer.Shutdown()
	cancel()
	cancelRuntime()

	if err := control.Close(); err != nil {
		retErr = errors.Join(retErr, err)
	}
	<-control.Done()

	runtimeLogger.Info("shutdown requested, stopping gRPC server")
	if stopGRPCServer(grpcServer, grpcShutdownTimeout) {
		runtimeLogger.Warn("gRPC graceful shutdown timed out", "timeout", grpcShutdownTimeout)
	}

	for completedWorkers < 2 {
		result := <-workerResults
		completedWorkers++
		if err := shutdownWorkerError(result); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}

	return
}

func unexpectedWorkerExit(result runtimeWorkerResult) error {
	if result.err == nil {
		return fmt.Errorf("%s worker stopped unexpectedly", result.name)
	}
	return result.err
}

func shutdownWorkerError(result runtimeWorkerResult) error {
	if result.err == nil || errors.Is(result.err, context.Canceled) || errors.Is(result.err, grpc.ErrServerStopped) {
		return nil
	}
	return fmt.Errorf("%s worker stopped: %w", result.name, result.err)
}

// stopGRPCServer gives in-flight RPCs a bounded grace period before forcing stop.
func stopGRPCServer(server grpcStopper, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return false
	case <-timer.C:
		server.Stop()
		<-done
		return true
	}
}
