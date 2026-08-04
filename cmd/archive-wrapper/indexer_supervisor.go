package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/node101-io/archive-wrapper/fetchmina"
	"github.com/node101-io/archive-wrapper/indexer"
)

// These defaults bound each probe and pace retryable Indexer failures.
const indexerRetryDelay = 2 * time.Second
const postgresProbeTimeout = 5 * time.Second

const archiveSourceBehindSummary = "archive source is behind indexed cursor"

// postgresPinger keeps the reconnect loop independent from pgxpool in tests.
type postgresPinger interface {
	Ping(context.Context) error
}

// supervisorPolicy controls probe deadlines and delay between retryable failures.
type supervisorPolicy struct {
	ProbeTimeout time.Duration
	RetryDelay   time.Duration
}

// indexerSession runs one Indexer lifecycle against one notification connection.
type indexerSession func(context.Context) error

// postgresNotificationConn is the subset of pgx.Conn required by the Indexer.
type postgresNotificationConn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	WaitForNotification(context.Context) (*pgconn.Notification, error)
	Close(context.Context) error
}

// notificationConnector allows connection establishment to be bounded and tested.
type notificationConnector func(context.Context, string) (postgresNotificationConn, error)

// superviseIndexer probes PostgreSQL and retries sessions only after connection loss.
func superviseIndexer(
	ctx context.Context,
	pinger postgresPinger,
	session indexerSession,
	readiness *readinessController,
	policy supervisorPolicy,
	runtimeLogger *slog.Logger,
) error {

	for {
		readiness.Connecting()
		attemptStartedAt := time.Now()
		// pgxpool.New is lazy, so Ping verifies connectivity before a session starts.
		probeStartedAt := time.Now()
		probeCtx, cancel := context.WithTimeout(ctx, policy.ProbeTimeout)
		err := pinger.Ping(probeCtx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			err = fmt.Errorf("%w: ping postgres query pool: %w", apperrors.ErrQueryConnectionLost, err)
		} else {
			runtimeLogger.Info("postgres query pool ping succeeded", "duration", time.Since(probeStartedAt))
			err = session(ctx)
		}

		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}

		switch {
		case errors.Is(err, apperrors.ErrArchiveTargetBehindCursor):
			readiness.WaitingForArchive(archiveSourceBehindSummary)
			runtimeLogger.Warn(
				"archive source is behind indexed cursor, waiting",
				"retry_delay",
				policy.RetryDelay,
				"error",
				err,
			)

		case errors.Is(err, apperrors.ErrNotificationConnectionLost),
			errors.Is(err, apperrors.ErrQueryConnectionLost):
			summary, _ := postgresErrorSummary(err)
			readiness.Reconnecting(summary)
			runtimeLogger.Warn("postgres connection lost, reconnecting", "duration", time.Since(attemptStartedAt), "retry_delay", policy.RetryDelay, "error", summary)

		default:
			readiness.Failed("indexer failed")
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(policy.RetryDelay):
		}
	}
}

// runIndexerSession owns the notification connection for one Indexer run.
func runIndexerSession(
	ctx context.Context,
	postgresURI string,
	client *fetchmina.MinaClient,
	db *database.DbManager,
	startBlockHeight int64,
	confirmationDepth int64,
	connectTimeout time.Duration,
	connect notificationConnector,
	observer indexer.SyncObserver,
	logger *slog.Logger,
	runtimeLogger *slog.Logger,
) error {
	runtimeLogger.Info("connecting postgres notification connection")

	connectStartedAt := time.Now()
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	notificationConn, err := connect(connectCtx, postgresURI)
	cancel()
	if err != nil {
		return fmt.Errorf("%w: connect notification connection: %w", apperrors.ErrNotificationConnectionLost, err)
	}
	runtimeLogger.Info("postgres notification connection ready", "duration", time.Since(connectStartedAt))
	defer closeNotificationConn(ctx, notificationConn, runtimeLogger)

	// Recreating the Indexer prevents a failed connection from leaking into a retry.
	idx, err := indexer.NewIndexer(
		notificationConn,
		client,
		db,
		startBlockHeight,
		confirmationDepth,
		logger,
		indexer.WithSyncObserver(observer),
	)
	if err != nil {
		return err
	}

	return idx.Run(ctx)
}

// connectPostgresNotification opens the dedicated LISTEN/NOTIFY connection.
func connectPostgresNotification(ctx context.Context, postgresURI string) (postgresNotificationConn, error) {
	return pgx.Connect(ctx, postgresURI)
}

// closeNotificationConn bounds shutdown so a broken connection cannot block exit.
func closeNotificationConn(ctx context.Context, conn postgresNotificationConn, logger *slog.Logger) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()

	if err := conn.Close(closeCtx); err != nil {
		logger.Warn("postgres notification connection close failed")
	}
}
