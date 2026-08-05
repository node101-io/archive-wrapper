package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/node101-io/archive-wrapper/apperrors"
)

// newRuntimeLogger defaults to stderr and adds an append-only file only when configured.
func newRuntimeLogger(
	stderr io.Writer,
	lookupEnv func(string) (string, bool),
) (*slog.Logger, func() error, error) {
	if stderr == nil {
		return nil, nil, errors.New("stderr writer is required")
	}
	if lookupEnv == nil {
		return nil, nil, errors.New("environment lookup is required")
	}

	writer := stderr
	logPath, configured := lookupEnv("ARCHIVE_WRAPPER_LOG_PATH")
	logPath = strings.TrimSpace(logPath)
	var logFile *os.File
	if configured && logPath != "" {
		var err error
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file %s: %w", logPath, err)
		}
		writer = io.MultiWriter(stderr, logFile)
	}

	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var closeOnce sync.Once
	var closeErr error
	cleanup := func() error {
		closeOnce.Do(func() {
			if logFile != nil {
				closeErr = logFile.Close()
			}
		})
		return closeErr
	}

	if logFile == nil {
		logger.Info("logger initialized", "destination", "stderr")
	} else {
		logger.Info("logger initialized", "destination", "stderr+file", "log_path", logPath)
	}
	return logger, cleanup, nil
}

// postgresErrorSummary projects credential-bearing driver errors to safe categories.
func postgresErrorSummary(err error) (string, bool) {
	switch {
	case errors.Is(err, apperrors.ErrPostgresConfigurationInvalid):
		return "postgres query pool configuration invalid", true
	case errors.Is(err, apperrors.ErrNotificationConnectionLost):
		return "postgres notification connection unavailable", true
	case errors.Is(err, apperrors.ErrQueryConnectionLost):
		return "postgres query connection unavailable", true
	default:
		return "", false
	}
}

// TODO(observability): Introduce a process-wide safe error logging contract
// before adding another credential-bearing backend. PostgreSQL errors are
// intentionally projected to allowlisted operational summaries here.
func logApplicationError(logger *slog.Logger, message string, err error) {
	if summary, ok := postgresErrorSummary(err); ok {
		logger.Error(message, "error", summary)
		return
	}
	logger.Error(message, "error", err)
}
