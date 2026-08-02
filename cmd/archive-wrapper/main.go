package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

const (
	exitCodeSuccess = 0
	exitCodeFailure = 1
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stderr))
}

func runMain(args []string, stderr io.Writer) (exitCode int) {
	if handled, code := runShortCommand(args, stderr, os.LookupEnv); handled {
		return code
	}

	logger, cleanup, err := newRuntimeLogger(stderr, os.LookupEnv)
	if err != nil {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "failed to initialize logger: %v\n", err)
		}
		return exitCodeFailure
	}
	logger = logger.With("version", version, "commit", commitSHA, "build_date", buildDate)
	slog.SetDefault(logger)
	exitCode = exitCodeSuccess
	defer func() {
		if err := cleanup(); err != nil {
			logger.Error("failed to close log file", "error", err)
			exitCode = exitCodeFailure
		}
	}()

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	logger.Info("archive-wrapper process started")
	if err := run(args, ctx, cancel, logger); err != nil && !errors.Is(err, context.Canceled) {
		logApplicationError(logger, "archive-wrapper process failed", err)
		return exitCodeFailure
	}

	logger.Info("archive-wrapper process stopped")
	return exitCodeSuccess
}
