package main

import (
	"context"
	"errors"
	"flag"
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
	"github.com/syndtr/goleveldb/leveldb"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	grpcHealth "google.golang.org/grpc/health"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

const queryGRPCServiceName = "query.Query"
const diagnosticsGRPCServiceName = "diagnostics.DiagnosticsService"

func run(args []string, ctx context.Context,
	cancel context.CancelFunc, logger *slog.Logger) error {
	cliLogger := logger.With("component", "cli")

	if len(args) == 0 {
		return fmt.Errorf("missing command\n\n%s", usage())
	}

	switch strings.ToLower(args[0]) {
	case "start":
		startCmd := flag.NewFlagSet("start", flag.ContinueOnError)

		defaultConfigPath := os.Getenv("ARCHIVE_WRAPPER_CONFIG")
		configPath := startCmd.String(
			"config",
			defaultConfigPath,
			"path to configuration file",
		)
		homePath := startCmd.String(
			"home",
			"",
			"path to chain home directory",
		)

		if err := startCmd.Parse(args[1:]); err != nil {
			return err
		}

		if strings.TrimSpace(*configPath) == "" {
			return fmt.Errorf("--config is required unless ARCHIVE_WRAPPER_CONFIG is set")
		}
		if strings.TrimSpace(*homePath) == "" {
			return fmt.Errorf("--home is required")
		}

		cliLogger.Info(
			"start command received",
			"config",
			*configPath,
			"home",
			*homePath,
		)

		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		if err := ensureDBPathDoesNotExist(cfg.DBPath); err != nil {
			return err
		}

		bridgeParams, err := loadBridgeParamsFromHome(*homePath)
		if err != nil {
			return fmt.Errorf("load bridge params from genesis: %w", err)
		}

		return runStart(ctx, cfg, bridgeParams, cancel, logger)

	case "proceed":
		proceedCmd := flag.NewFlagSet("proceed", flag.ContinueOnError)

		defaultConfigPath := os.Getenv("ARCHIVE_WRAPPER_CONFIG")
		configPath := proceedCmd.String(
			"config",
			defaultConfigPath,
			"path to configuration file",
		)
		homePath := proceedCmd.String(
			"home",
			"",
			"path to chain home directory",
		)

		if err := proceedCmd.Parse(args[1:]); err != nil {
			return err
		}

		if strings.TrimSpace(*configPath) == "" {
			return fmt.Errorf("--config is required unless ARCHIVE_WRAPPER_CONFIG is set")
		}
		if strings.TrimSpace(*homePath) == "" {
			return fmt.Errorf("--home is required")
		}

		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		if _, err := os.Stat(cfg.DBPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("db does not exist: %s; run start first", cfg.DBPath)
			}
			return fmt.Errorf("stat db path: %w", err)
		}

		latestProcessedBlockHeight, err := loadLatestProcessedBlockHeight(
			cfg.DBPath,
			cfg.BlockHeightDatabaseKey,
			logger,
		)
		// A missing cursor is valid when initialization finished before the first block.
		if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
			return fmt.Errorf("load latest processed block height: %w", err)
		}

		cliLogger.Info(
			"proceed command received",
			"config",
			*configPath,
			"home",
			*homePath,
		)
		if errors.Is(err, leveldb.ErrNotFound) {
			cliLogger.Info("resuming before first cursor", "db_path", cfg.DBPath)
		} else {
			cliLogger.Info(
				"resuming from persisted block height",
				"block_height",
				latestProcessedBlockHeight,
				"db_path",
				cfg.DBPath,
			)
		}

		bridgeParams, err := loadBridgeParamsFromHome(*homePath)
		if err != nil {
			return fmt.Errorf("load bridge params from genesis: %w", err)
		}

		// Keep the genesis start height for bounds validation; the indexer
		// resumes from the persisted cursor automatically when it exists.
		return runStart(ctx, cfg, bridgeParams, cancel, logger)

	case "stop":
		stopCmd := flag.NewFlagSet("stop", flag.ContinueOnError)

		defaultConfigPath := os.Getenv("ARCHIVE_WRAPPER_CONFIG")
		configPath := stopCmd.String(
			"config",
			defaultConfigPath,
			"path to configuration file",
		)

		socketPath := stopCmd.String(
			"socket-path",
			"",
			"path to control socket",
		)

		if err := stopCmd.Parse(args[1:]); err != nil {
			return err
		}

		resolvedSocketPath, err := resolveStopSocketPath(
			*socketPath,
			*configPath,
			os.Getenv("ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH"),
		)
		if err != nil {
			return err
		}

		cliLogger.Info(
			"stop command received",
			"config",
			strings.TrimSpace(*configPath),
			"socket_path",
			resolvedSocketPath,
		)

		return runStop(resolvedSocketPath, logger)

	case "help", "-h", "--help":
		fmt.Print(usage())
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

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
	healthServer.SetServingStatus("", grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(queryGRPCServiceName, grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(diagnosticsGRPCServiceName, grpcHealthV1.HealthCheckResponse_SERVING)

	return healthServer
}

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

	db, err := database.NewDbManager(cfg.DBPath, cfg.BlockHeightDatabaseKey, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close db: %w", err))
		}
	}()

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
		return err
	}
	runtimeLogger.Info("postgres query pool configured")
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

	group, runCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		runtimeLogger.Info("starting indexer run loop")

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
			runtimeLogger.Error("indexer run loop stopped with error", "err", err)
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

func usage() string {
	return `usage:
  archive-wrapper start --config <path> --home <path>
  archive-wrapper proceed --config <path> --home <path>
  archive-wrapper stop [--config <path>] [--socket-path <path>]
`
}
