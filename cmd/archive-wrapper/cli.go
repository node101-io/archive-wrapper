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
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/config"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/node101-io/archive-wrapper/fetchmina"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/node101-io/archive-wrapper/indexer"
	"github.com/node101-io/archive-wrapper/query"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

const network = "unix"

func run(args []string, ctx context.Context,
	cancel context.CancelFunc, logger *slog.Logger) error {
	cliLogger := logger.With("component", "cli")

	if len(args) == 0 {
		return fmt.Errorf("missing command\n\n%s", usage())
	}

	switch strings.ToLower(args[0]) {
	case "start":
		startCmd := flag.NewFlagSet("start", flag.ContinueOnError)

		startBlockHeight := startCmd.Int64(
			"start-block-height",
			0,
			"first block height to start indexing from",
		)

		defaultConfigPath := os.Getenv("ARCHIVE_WRAPPER_CONFIG")
		configPath := startCmd.String(
			"config",
			defaultConfigPath,
			"path to configuration file",
		)

		if err := startCmd.Parse(args[1:]); err != nil {
			return err
		}

		if *startBlockHeight <= 0 {
			return fmt.Errorf("--start-block-height is required and must be greater than 0")
		}

		cliLogger.Info(
			"start command received",
			"config",
			*configPath,
			"start_block_height",
			*startBlockHeight,
		)

		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}

		return runStart(ctx, cfg, *startBlockHeight, cancel, logger)

	case "stop":
		stopCmd := flag.NewFlagSet("stop", flag.ContinueOnError)

		defaultSocketPath := os.Getenv("ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH")
		socketPath := stopCmd.String(
			"socket-path",
			defaultSocketPath,
			"path to control socket",
		)

		if err := stopCmd.Parse(args[1:]); err != nil {
			return err
		}

		cliLogger.Info("stop command received", "socket_path", *socketPath)

		return runStop(*socketPath, logger)

	case "help", "-h", "--help":
		fmt.Print(usage())
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}
func runStart(ctx context.Context, cfg config.Config,
	startBlockHeight int64, cancel context.CancelFunc, logger *slog.Logger) (retErr error) {
	runtimeLogger := logger.With("component", "runtime")

	runtimeLogger.Info(
		"starting archive wrapper",
		"start_block_height",
		startBlockHeight,
		"grpc_listen_address",
		cfg.GRPCListenAddress,
		"control_socket_path",
		cfg.ControlSocketPath,
		"db_path",
		cfg.DBPath,
	)

	ln, err := listenControlSocket(cfg.ControlSocketPath, cancel, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := ln.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close control socket listener: %w", err))
		}
		if err := os.Remove(cfg.ControlSocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			retErr = errors.Join(retErr, fmt.Errorf("remove control socket: %w", err))
		}
	}()

	postgresURI := strings.TrimSpace(os.Getenv("POSTGRES_URI"))
	if postgresURI == "" {
		return apperrors.ErrPostgreUriRequired
	}

	runtimeLogger.Info("connecting to postgres")

	notificationConn, err := pgx.Connect(ctx, postgresURI)
	if err != nil {
		return err
	}
	runtimeLogger.Info("postgres notification connection ready")
	defer func() {
		if err := notificationConn.Close(context.Background()); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close notification connection: %w", err))
		}
	}()

	queryPool, err := pgxpool.New(ctx, postgresURI)
	if err != nil {
		return err
	}
	runtimeLogger.Info("postgres query pool ready")
	defer queryPool.Close()

	client, err := fetchmina.NewMinaClient(
		cfg.ContractAddress,
		sqlcdb.New(queryPool),
		logger,
	)
	if err != nil {
		return err
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

	indexer, err := indexer.NewIndexer(
		notificationConn,
		client,
		db,
		startBlockHeight,
		cfg.ConfirmationDepth,
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
	queryService, err := query.NewQuery(db, logger)
	if err != nil {
		return err
	}
	query.RegisterQueryServer(grpcServer, queryService)

	group, runCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		runtimeLogger.Info("starting indexer run loop")

		err := indexer.Run(runCtx)
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

func runStop(sockPath string, logger *slog.Logger) (retErr error) {
	cliLogger := logger.With("component", "cli")

	cliLogger.Info("sending stop request", "socket_path", sockPath)

	c, err := net.DialTimeout(network, sockPath, time.Second)
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close stop connection: %w", err))
		}
	}()

	cliLogger.Info("stop request sent", "socket_path", sockPath)

	return
}
func listenControlSocket(sockPath string, cancel context.CancelFunc, logger *slog.Logger) (net.Listener, error) {
	controlLogger := logger.With("component", "control_socket")

	if err := prepareControlSocket(sockPath, logger); err != nil {
		return nil, err
	}

	ln, err := net.Listen(network, sockPath)
	if err != nil {
		return nil, err
	}

	controlLogger.Info("control socket listening", "socket_path", sockPath)

	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = c.Close()
			controlLogger.Info("stop request received via control socket", "socket_path", sockPath)
			cancel()
			return
		}
		if !errors.Is(err, net.ErrClosed) {
			controlLogger.Error("control socket accept failed", "socket_path", sockPath, "err", err)
		}
	}()

	return ln, nil
}

func prepareControlSocket(sockPath string, logger *slog.Logger) error {
	controlLogger := logger.With("component", "control_socket")

	info, err := os.Lstat(sockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a unix socket", sockPath)
	}

	c, err := net.DialTimeout(network, sockPath, 300*time.Millisecond)
	if err == nil {
		_ = c.Close()
		controlLogger.Warn("control socket already in use", "socket_path", sockPath)
		return fmt.Errorf("control socket already in use: %s", sockPath)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) || errors.Is(opErr.Err, syscall.ENOENT) {
			controlLogger.Warn("removing stale control socket", "socket_path", sockPath)
			if err := os.Remove(sockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("probe control socket %s: %w", sockPath, err)
}

func usage() string {
	return `usage:
  archive-wrapper start --config <path> --start-block-height <height>
  archive-wrapper stop --socket-path <path>
`
}
