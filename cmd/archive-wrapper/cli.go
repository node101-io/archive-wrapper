package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/config"
)

// run validates command-specific inputs before handing control to the runtime.
func run(args []string, ctx context.Context, cancel context.CancelFunc, logger *slog.Logger) error {
	cliLogger := logger.With("component", "cli")

	if len(args) == 0 {
		return fmt.Errorf("missing command\n\n%s", usage())
	}

	switch strings.ToLower(args[0]) {
	case "run":
		cmd := newRuntimeFlagSet("run")
		if err := cmd.Parse(args[1:]); err != nil {
			return err
		}
		if cmd.NArg() != 0 {
			return fmt.Errorf("run does not accept positional arguments: %q", cmd.Args())
		}
		configPath, err := resolveConfigPath(cmd.FlagSet, *cmd.configPath)
		if err != nil {
			return err
		}
		cfg, err := config.Resolve(configPath, cmd.overrides())
		if err != nil {
			return err
		}

		params, err := loadBridgeParamsFromHome(cfg.ChainHome)
		if err != nil {
			return fmt.Errorf("load bridge params from genesis: %w", err)
		}

		postgresURI := strings.TrimSpace(os.Getenv("POSTGRES_URI"))
		if postgresURI == "" {
			return apperrors.ErrPostgresURIRequired
		}

		cliLogger.Info("run command received", "config", configPath, "home", cfg.ChainHome)
		return runRuntime(ctx, runtimeInputs{
			Config:       cfg,
			BridgeParams: params,
			PostgresURI:  postgresURI,
		}, cancel, logger)

	case "stop":
		cmd := flag.NewFlagSet("stop", flag.ContinueOnError)
		configPath := cmd.String("config", "", "path to configuration file")
		controlSocketPath := cmd.String("control-socket-path", "", "path to control socket")
		if err := cmd.Parse(args[1:]); err != nil {
			return err
		}
		if cmd.NArg() != 0 {
			return fmt.Errorf("stop does not accept positional arguments: %q", cmd.Args())
		}

		resolvedSocketPath, err := resolveStopSocketPath(
			*controlSocketPath,
			flagWasSet(cmd, "control-socket-path"),
			*configPath,
			flagWasSet(cmd, "config"),
		)
		if err != nil {
			return err
		}

		cliLogger.Info("stop command received", "config", strings.TrimSpace(*configPath), "socket_path", resolvedSocketPath)
		return runStop(resolvedSocketPath, logger)

	case "help", "-h", "--help":
		fmt.Print(usage())
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

type runtimeFlagSet struct {
	*flag.FlagSet
	configPath        *string
	homePath          *string
	listenAddress     *string
	transportMode     *string
	dbPath            *string
	controlSocketPath *string
}

func newRuntimeFlagSet(name string) *runtimeFlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return &runtimeFlagSet{
		FlagSet:           fs,
		configPath:        fs.String("config", "", "path to configuration file"),
		homePath:          fs.String("home", "", "path to chain home directory"),
		listenAddress:     fs.String("grpc-listen-address", "", "gRPC listen address"),
		transportMode:     fs.String("grpc-transport-mode", "", "gRPC transport mode"),
		dbPath:            fs.String("db-path", "", "LevelDB path"),
		controlSocketPath: fs.String("control-socket-path", "", "control socket path"),
	}
}

func (f *runtimeFlagSet) overrides() config.Overrides {
	return config.Overrides{
		ChainHome:         visitedStringFlag(f.FlagSet, "home", f.homePath),
		GRPCListenAddress: visitedStringFlag(f.FlagSet, "grpc-listen-address", f.listenAddress),
		GRPCTransportMode: visitedStringFlag(f.FlagSet, "grpc-transport-mode", f.transportMode),
		DBPath:            visitedStringFlag(f.FlagSet, "db-path", f.dbPath),
		ControlSocketPath: visitedStringFlag(f.FlagSet, "control-socket-path", f.controlSocketPath),
	}
}

func usage() string {
	return `usage:
  archive-wrapper run [--config <path>] [--home <path>] [runtime overrides]
  archive-wrapper stop [--config <path>] [--control-socket-path <path>]
`
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	wasSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			wasSet = true
		}
	})
	return wasSet
}

func visitedStringFlag(fs *flag.FlagSet, name string, value *string) *string {
	if !flagWasSet(fs, name) {
		return nil
	}
	return value
}

func resolveConfigPath(fs *flag.FlagSet, flagValue string) (string, error) {
	if flagWasSet(fs, "config") {
		if strings.TrimSpace(flagValue) == "" {
			return "", apperrors.ErrConfigPathRequired
		}
		return strings.TrimSpace(flagValue), nil
	}
	if value, ok := os.LookupEnv("ARCHIVE_WRAPPER_CONFIG"); ok {
		if strings.TrimSpace(value) == "" {
			return "", apperrors.ErrConfigPathRequired
		}
		return strings.TrimSpace(value), nil
	}
	return "", apperrors.ErrConfigPathRequired
}
