package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"

	"github.com/jackc/pgx/v5"
	"github.com/node101-io/archive-wrapper/config"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/node101-io/archive-wrapper/fetchmina"
	"github.com/node101-io/archive-wrapper/indexer"
)

func run(args []string, ctx context.Context, cfg config.Config) error {
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

		if err := startCmd.Parse(args[1:]); err != nil {
			return err
		}

		if *startBlockHeight <= 0 {
			return fmt.Errorf("--start-block-height is required and must be greater than 0")
		}

		return runStart(ctx, cfg, *startBlockHeight)

	case "stop":
		runStop()
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage())
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func runStart(ctx context.Context, cfg config.Config, startBlockHeight int64) error {

	postgreUri := os.Getenv("POSTGRES_URI")

	conn, err := pgx.Connect(ctx, postgreUri)
	if err != nil {
		return err
	}

	client, err := fetchmina.NewMinaClient(cfg.ContractAddress, sqlcdb.New(conn))
	if err != nil {
		return err
	}

	db, err := database.NewDbManager(cfg.DBPath)
	if err != nil {
		return err
	}

	indexer, err := indexer.NewIndexer(conn, client, db, startBlockHeight, cfg.ConfirmationDepth, ctx)
	if err != nil {
		return err
	}

	return indexer.Run(ctx)
}

func runStop() {

}

func usage() string {
	return `usage:
  archive-wrapper start --start-block-height <height>
  archive-wrapper stop
`
}
