package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

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

const sockPath = "/tmp/archive-wrapper.sock"
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

		configPath := startCmd.String(
			"config",
			"",
			"path to configuration file",
		)

		if err := startCmd.Parse(args[1:]); err != nil {
			return err
		}

		if *startBlockHeight <= 0 {
			return fmt.Errorf("--start-block-height is required and must be greater than 0")
		}

		if *configPath == "" {
			return fmt.Errorf("--config is required")
		}

		// stop command should not need to depend on config.Load's success. Hence, i moved it here.
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}

		return runStart(ctx, cfg, *startBlockHeight, cancel)

	case "stop":
		return runStop()
	case "help", "-h", "--help":
		fmt.Print(usage())
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func runStart(ctx context.Context, cfg config.Config,
	startBlockHeight int64, cancel context.CancelFunc) error {

	os.Remove(sockPath)

	ln, err := net.Listen(network, sockPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
			cancel()
		}
	}()

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

func runStop() error {

	c, err := net.Dial(network, sockPath)
	if err != nil {
		return err
	}
	defer c.Close()

	return nil
}

func usage() string {
	return `usage:
  archive-wrapper start --config <path> --start-block-height <height>
  archive-wrapper stop
`
}
