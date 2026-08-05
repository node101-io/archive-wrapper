package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/config"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/node101-io/archive-wrapper/diagnostics"
	"github.com/node101-io/archive-wrapper/fetchmina"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/node101-io/archive-wrapper/query"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	grpcHealth "google.golang.org/grpc/health"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Service names must match the generated descriptors used by gRPC health clients.
const queryGRPCServiceName = "query.Query"
const diagnosticsGRPCServiceName = "diagnostics.DiagnosticsService"

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

// runStart builds the long-lived runtime and coordinates all component lifecycles.
func runStart(ctx context.Context, cfg config.Config,
	bridgeParams bridgeParams, cancel context.CancelFunc, logger *slog.Logger) (retErr error) {
	if err := cfg.Validate(); err != nil {
		return err
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
	if err := db.EnsureDeploymentMetadata(cfg.DeploymentMetadataKey, cfg.DeploymentMetadata); err != nil {
		return err
	}

	ln, err := listenControlSocket(cfg.ControlSocketPath, cancel, logger)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, closeControlSocketListener(ln))
	}()

	postgresURI := strings.TrimSpace(os.Getenv("POSTGRES_URI"))
	if postgresURI == "" {
		return apperrors.ErrPostgresURIRequired
	}

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

	// A failure in any worker cancels the shared runtime context.
	group, runCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
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
		if err != nil {
			return err
		}

		runtimeLogger.Info("indexer run loop stopped")
		return nil
	})

	group.Go(func() error {
		runtimeLogger.Info("gRPC server listening", "address", cfg.GRPCListenAddress)

		err := grpcServer.Serve(grpcListener)
		if err != nil {
			runtimeLogger.Error("gRPC server stopped with error", "err", err)
			return err
		}

		runtimeLogger.Info("gRPC server stopped")
		return nil
	})

	group.Go(func() error {
		<-runCtx.Done()
		// Publish unavailability before waiting for in-flight RPCs to finish.
		runtimeLogger.Info("shutdown requested, updating gRPC health status")
		readiness.Stopping()
		healthServer.Shutdown()
		// Let in-flight RPCs finish before the server stops.
		runtimeLogger.Info("shutdown requested, stopping gRPC server")
		grpcServer.GracefulStop()
		return nil
	})

	retErr = group.Wait()
	if errors.Is(retErr, context.Canceled) {
		retErr = nil
	}

	return
}
