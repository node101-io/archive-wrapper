package main

import (
	"errors"
	"log/slog"

	"github.com/node101-io/archive-wrapper/apperrors"
)

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
