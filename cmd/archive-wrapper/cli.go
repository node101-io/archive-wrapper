package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
const notificationReconnectDelay = 2 * time.Second

const (
	controlSocketReadTimeout  = time.Second
	controlSocketPingCommand  = "PING\n"
	controlSocketPongResponse = "PONG\n"
	controlSocketStopCommand  = "STOP\n"
)

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

		if *startBlockHeight <= 0 {
			return fmt.Errorf("--start-block-height is required and must be greater than 0")
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
			"start_block_height",
			*startBlockHeight,
		)

		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}

		contractAddress, err := LoadContractAddressFromHome(*homePath)
		if err != nil {
			return fmt.Errorf("load chain contract address: %w", err)
		}
		confirmationDepth, err := LoadConfirmationDepthFromHome(*homePath)
		if err != nil {
			return fmt.Errorf("load chain confirmation depth: %w", err)
		}

		return runStart(ctx, cfg, *startBlockHeight, contractAddress, confirmationDepth, cancel, logger)

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
func runStart(ctx context.Context, cfg config.Config,
	startBlockHeight int64, contractAddress string, confirmationDepth int64, cancel context.CancelFunc, logger *slog.Logger) (retErr error) {
	runtimeLogger := logger.With("component", "runtime")

	runtimeLogger.Info(
		"starting archive wrapper",
		"start_block_height",
		startBlockHeight,
		"grpc_listen_address",
		cfg.GRPCListenAddress,
		"control_socket_path",
		cfg.ControlSocketPath,
		"confirmation_depth",
		confirmationDepth,
		"db_path",
		cfg.DBPath,
	)

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
	runtimeLogger.Info("postgres query pool ready")
	defer queryPool.Close()

	client, err := fetchmina.NewMinaClient(
		contractAddress,
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
	queryService, err := query.NewQuery(db, logger, cfg.MaxActionRangeHeights)
	if err != nil {
		return err
	}
	query.RegisterQueryServer(grpcServer, queryService)

	group, runCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		runtimeLogger.Info("starting indexer run loop")

		err := runIndexerWithReconnect(
			runCtx,
			postgresURI,
			client,
			db,
			startBlockHeight,
			confirmationDepth,
			logger,
		)
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

func closeControlSocketListener(ln net.Listener) error {
	if err := ln.Close(); err != nil {
		return fmt.Errorf("close control socket listener: %w", err)
	}
	return nil
}

func runIndexerWithReconnect(
	ctx context.Context,
	postgresURI string,
	client *fetchmina.MinaClient,
	db *database.DbManager,
	startBlockHeight int64,
	confirmationDepth int64,
	logger *slog.Logger,
) error {
	runtimeLogger := logger.With("component", "runtime")

	for {
		err := runIndexerOnce(
			ctx,
			postgresURI,
			client,
			db,
			startBlockHeight,
			confirmationDepth,
			logger,
			runtimeLogger,
		)

		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}

		if !errors.Is(err, apperrors.ErrNotificationConnectionLost) &&
			!errors.Is(err, apperrors.ErrQueryConnectionLost) {
			return err
		}

		runtimeLogger.Warn("postgres connection lost, reconnecting", "retry_delay", notificationReconnectDelay, "err", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(notificationReconnectDelay):
		}
	}
}

func runIndexerOnce(
	ctx context.Context,
	postgresURI string,
	client *fetchmina.MinaClient,
	db *database.DbManager,
	startBlockHeight int64,
	confirmationDepth int64,
	logger *slog.Logger,
	runtimeLogger *slog.Logger,
) error {
	runtimeLogger.Info("connecting postgres notification connection")

	notificationConn, err := pgx.Connect(ctx, postgresURI)
	if err != nil {
		return fmt.Errorf("%w: connect notification connection: %w", apperrors.ErrNotificationConnectionLost, err)
	}
	runtimeLogger.Info("postgres notification connection ready")
	defer closeNotificationConn(ctx, notificationConn, runtimeLogger)

	idx, err := indexer.NewIndexer(
		notificationConn,
		client,
		db,
		startBlockHeight,
		confirmationDepth,
		logger,
	)
	if err != nil {
		return err
	}

	return idx.Run(ctx)
}

func closeNotificationConn(ctx context.Context, conn *pgx.Conn, logger *slog.Logger) {
	closeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	if err := conn.Close(closeCtx); err != nil {
		logger.Warn("close notification connection failed", "err", err)
	}
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

	if _, err := io.WriteString(c, controlSocketStopCommand); err != nil {
		return fmt.Errorf("write stop command: %w", err)
	}

	cliLogger.Info("stop request sent", "socket_path", sockPath)

	return
}

func resolveStopSocketPath(socketPath, configPath, envSocketPath string) (string, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath != "" {
		return socketPath, nil
	}

	configPath = strings.TrimSpace(configPath)
	if configPath != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return "", fmt.Errorf("load stop config: %w", err)
		}
		return cfg.ControlSocketPath, nil
	}

	envSocketPath = strings.TrimSpace(envSocketPath)
	if envSocketPath != "" {
		return envSocketPath, nil
	}

	return "", fmt.Errorf(
		"--config or --socket-path is required unless ARCHIVE_WRAPPER_CONFIG or ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH is set",
	)
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
		for {
			c, err := ln.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					controlLogger.Error("control socket accept failed", "socket_path", sockPath, "err", err)
				}
				return
			}

			shouldStop := handleControlSocketConn(c, sockPath, controlLogger)
			if shouldStop {
				controlLogger.Info("stop request received via control socket", "socket_path", sockPath)
				cancel()
				return
			}
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
		if err := probeLiveControlSocket(c); err != nil {
			return fmt.Errorf("probe control socket %s: %w", sockPath, err)
		}
		controlLogger.Warn("control socket already in use", "socket_path", sockPath)
		return fmt.Errorf("control socket already in use: %s", sockPath)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) || errors.Is(opErr.Err, syscall.ENOENT) {
			// Refused or missing means the old socket file is stale and safe to remove.
			controlLogger.Warn("removing stale control socket", "socket_path", sockPath)
			if err := os.Remove(sockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("probe control socket %s: %w", sockPath, err)
}

func handleControlSocketConn(c net.Conn, sockPath string, logger *slog.Logger) bool {
	defer func() {
		if err := c.Close(); err != nil {
			logger.Warn("close control socket connection failed", "socket_path", sockPath, "err", err)
		}
	}()

	if err := c.SetDeadline(time.Now().Add(controlSocketReadTimeout)); err != nil {
		logger.Warn("set control socket deadline failed", "socket_path", sockPath, "err", err)
		return false
	}

	command, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			logger.Warn("control socket connection closed without command", "socket_path", sockPath)
			return false
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			logger.Warn("control socket command timed out", "socket_path", sockPath)
			return false
		}
		logger.Warn("read control socket command failed", "socket_path", sockPath, "err", err)
		return false
	}

	switch strings.TrimSpace(command) {
	case "PING":
		if _, err := io.WriteString(c, controlSocketPongResponse); err != nil {
			logger.Warn("write control socket pong failed", "socket_path", sockPath, "err", err)
		}
		return false
	case "STOP":
		return true
	default:
		logger.Warn("unknown control socket command", "socket_path", sockPath, "command", strings.TrimSpace(command))
		return false
	}
}

func probeLiveControlSocket(c net.Conn) error {
	defer c.Close()

	if err := c.SetDeadline(time.Now().Add(controlSocketReadTimeout)); err != nil {
		return fmt.Errorf("set control socket probe deadline: %w", err)
	}

	if _, err := io.WriteString(c, controlSocketPingCommand); err != nil {
		return fmt.Errorf("write control socket ping: %w", err)
	}

	response, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read control socket pong: %w", err)
	}
	if response != controlSocketPongResponse {
		return fmt.Errorf("unexpected control socket response %q", strings.TrimSpace(response))
	}

	return nil
}

func usage() string {
	return `usage:
  archive-wrapper start --config <path> --home <path> --start-block-height <height>
  archive-wrapper stop [--config <path>] [--socket-path <path>]
`
}
