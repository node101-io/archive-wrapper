package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/node101-io/archive-wrapper/config"
	"github.com/syndtr/goleveldb/leveldb"
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

func usage() string {
	return `usage:
  archive-wrapper start --config <path> --home <path>
  archive-wrapper proceed --config <path> --home <path>
  archive-wrapper stop [--config <path>] [--socket-path <path>]
`
}
