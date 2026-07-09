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

	"github.com/joho/godotenv"
)

func main() {

	err := godotenv.Load()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to load environment file: %v\n", err)
		os.Exit(1)
	}

	logPath := os.Getenv("ARCHIVE_WRAPPER_LOG_PATH")
	if logPath == "" {
		logPath = "archive-wrapper.log"
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to open log file %s: %v\n", logPath, err)
		os.Exit(1)
	}

	logger := slog.New(
		slog.NewTextHandler(
			io.MultiWriter(os.Stdout, logFile),
			&slog.HandlerOptions{Level: slog.LevelInfo},
		),
	)
	slog.SetDefault(logger)
	defer func() {
		if err := logFile.Close(); err != nil {
			logger.Error("failed to close log file", "log_path", logPath, "err", err)
		}
	}()

	logger.Info("logger initialized", "log_path", logPath)

	// allows to close the wrapper via CTRL + C
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Allows graceful shutdown with cli command
	ctx, cancel := context.WithCancel(ctx)

	logger.Info("archive-wrapper process started")

	if err := run(os.Args[1:], ctx, cancel, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("archive-wrapper process failed", "err", err)
		os.Exit(1)
	}

	logger.Info("archive-wrapper process stopped")
}
