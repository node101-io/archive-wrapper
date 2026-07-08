package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	cancel context.CancelFunc) error {
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

		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}

		return runStart(ctx, cfg, *startBlockHeight, cancel)

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

		return runStop(*socketPath)

	case "help", "-h", "--help":
		fmt.Print(usage())
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func runStart(ctx context.Context, cfg config.Config, startBlockHeight int64, cancel context.CancelFunc) error {
	ln, err := listenControlSocket(cfg.ControlSocketPath, cancel)
	if err != nil {
		return err
	}
	defer cleanupControlSocket(ln, cfg.ControlSocketPath)

	postgreUri := os.Getenv("POSTGRES_URI")

	notificationConn, err := pgx.Connect(ctx, postgreUri)
	if err != nil {
		return err
	}
	defer notificationConn.Close(context.Background())

	queryPool, err := pgxpool.New(ctx, postgreUri)
	if err != nil {
		return err
	}
	defer queryPool.Close()

	client, err := fetchmina.NewMinaClient(
		cfg.ContractAddress,
		sqlcdb.New(queryPool),
	)
	if err != nil {
		return err
	}

	db, err := database.NewDbManager(cfg.DBPath, cfg.BlockHeightDatabaseKey)
	if err != nil {
		return err
	}
	defer db.Close()

	indexer, err := indexer.NewIndexer(notificationConn, client, db, startBlockHeight, cfg.ConfirmationDepth, ctx)
	if err != nil {
		return err
	}

	grpcListener, err := net.Listen("tcp", cfg.GRPCListenAddress)
	if err != nil {
		return fmt.Errorf("listen gRPC: %w", err)
	}
	defer grpcListener.Close()

	grpcServer := grpc.NewServer()
	query.RegisterQueryServer(grpcServer, query.NewQuery(db))

	group, runCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		return indexer.Run(runCtx)
	})

	group.Go(func() error {
		return grpcServer.Serve(grpcListener)
	})

	group.Go(func() error {
		<-runCtx.Done()
		grpcServer.GracefulStop()
		return nil
	})

	return group.Wait()
}

func runStop(sockPath string) error {
	c, err := net.DialTimeout(network, sockPath, time.Second)
	if err != nil {
		return err
	}
	defer c.Close()

	return nil
}

func listenControlSocket(sockPath string, cancel context.CancelFunc) (net.Listener, error) {
	if err := prepareControlSocket(sockPath); err != nil {
		return nil, err
	}

	ln, err := net.Listen(network, sockPath)
	if err != nil {
		return nil, err
	}

	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = c.Close()
			cancel()
		}
	}()

	return ln, nil
}

func cleanupControlSocket(ln net.Listener, sockPath string) {
	_ = ln.Close()
	_ = os.Remove(sockPath)
}

func prepareControlSocket(sockPath string) error {
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
		return fmt.Errorf("control socket already in use: %s", sockPath)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) || errors.Is(opErr.Err, syscall.ENOENT) {
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
