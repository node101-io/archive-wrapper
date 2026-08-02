package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/stretchr/testify/require"
)

func TestNewRuntimeLoggerDefaultsToStderr(t *testing.T) {
	var stderr bytes.Buffer
	logger, cleanup, err := newRuntimeLogger(&stderr, func(string) (string, bool) {
		return "", false
	})
	require.NoError(t, err)
	require.NotNil(t, logger)
	logger.Info("runtime message")
	require.NoError(t, cleanup())
	require.NoError(t, cleanup(), "cleanup must be idempotent")
	require.Contains(t, stderr.String(), "destination=stderr")
	require.Contains(t, stderr.String(), "runtime message")
}

func TestNewRuntimeLoggerTeesToExplicitPrivateFile(t *testing.T) {
	var stderr bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "wrapper.log")
	logger, cleanup, err := newRuntimeLogger(&stderr, func(key string) (string, bool) {
		require.Equal(t, "ARCHIVE_WRAPPER_LOG_PATH", key)
		return logPath, true
	})
	require.NoError(t, err)
	logger.Info("tee message")
	require.NoError(t, cleanup())

	contents, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(contents), "tee message")
	require.Contains(t, stderr.String(), "tee message")
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestNewRuntimeLoggerRejectsInvalidLogPath(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(parentFile, []byte("file"), 0o600))
	logPath := filepath.Join(parentFile, "wrapper.log")

	logger, cleanup, err := newRuntimeLogger(&bytes.Buffer{}, func(string) (string, bool) {
		return logPath, true
	})
	require.Error(t, err)
	require.Nil(t, logger)
	require.Nil(t, cleanup)
}

func TestRunMainReturnsFailureWithoutCreatingDefaultLogFile(t *testing.T) {
	unsetEnvironment(t, "ARCHIVE_WRAPPER_LOG_PATH")
	workingDirectory := t.TempDir()
	originalWorkingDirectory, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workingDirectory))
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalWorkingDirectory)) })
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDirectory, ".env"),
		[]byte("ARCHIVE_WRAPPER_LOG_PATH=from-dotenv.log\n"),
		0o600,
	))
	var stderr bytes.Buffer

	exitCode := runMain(nil, &stderr)
	require.Equal(t, exitCodeFailure, exitCode)
	require.Contains(t, stderr.String(), "destination=stderr")
	require.Contains(t, stderr.String(), "missing command")
	_, err = os.Stat(filepath.Join(workingDirectory, "archive-wrapper.log"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(workingDirectory, "from-dotenv.log"))
	require.ErrorIs(t, err, os.ErrNotExist, "the binary must not load .env files")
}

func TestPostgresErrorSummaryPreservesTypedCategories(t *testing.T) {
	err := fmt.Errorf("%w: %w", apperrors.ErrQueryConnectionLost, errors.New("postgres://user:secret@db/archive"))

	summary, ok := postgresErrorSummary(err)
	require.True(t, ok)
	require.Equal(t, "postgres query connection unavailable", summary)
	require.ErrorIs(t, err, apperrors.ErrQueryConnectionLost)
}

func TestLogApplicationErrorDoesNotWritePostgresDetails(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	secretURI := "postgres://user:p%40ss@db.internal/archive?sslmode=disable"
	err := fmt.Errorf("%w: %w", apperrors.ErrNotificationConnectionLost, errors.New(secretURI))

	logApplicationError(logger, "connection failed", err)

	logOutput := output.String()
	require.Contains(t, logOutput, "postgres notification connection unavailable")
	require.NotContains(t, logOutput, secretURI)
	require.NotContains(t, logOutput, "p%40ss")
	require.NotContains(t, logOutput, "p@ss")
	require.False(t, strings.Contains(logOutput, "db.internal"))
}

func TestLogApplicationErrorPreservesNonPostgresErrorDetails(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))

	logApplicationError(logger, "application failed", errors.New("unexpected failure"))

	require.Contains(t, output.String(), "unexpected failure")
}
