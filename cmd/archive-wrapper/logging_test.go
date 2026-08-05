package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/stretchr/testify/require"
)

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
