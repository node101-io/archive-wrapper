package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/diagnostics"
	"github.com/node101-io/archive-wrapper/indexer"
	"github.com/stretchr/testify/require"
)

func TestSuperviseIndexerPingsBeforeSessionAndRecovers(t *testing.T) {
	_, store, controller := newTestReadinessController()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := make([]string, 0, 3)
	var mu sync.Mutex
	pinger := pingerFunc(func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "ping")
		if len(events) == 1 {
			return errors.New("unavailable")
		}
		return nil
	})
	session := func(context.Context) error {
		mu.Lock()
		events = append(events, "session")
		mu.Unlock()
		return nil
	}

	err := superviseIndexer(context.Background(), pinger, session, controller, reconnectPolicy{
		ProbeTimeout: time.Second,
		RetryDelay:   time.Millisecond,
	}, logger)
	require.NoError(t, err)
	require.Equal(t, []string{"ping", "ping", "session"}, events)
	require.Equal(t, "postgres query connection unavailable", store.Snapshot().LastError)
	require.False(t, store.Snapshot().Ready, "Ping alone must not make the service ready")
}

func TestSuperviseIndexerBoundsPingWithTimeout(t *testing.T) {
	_, _, controller := newTestReadinessController()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var calls int
	pinger := pingerFunc(func(ctx context.Context) error {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	sessionCalls := 0
	startedAt := time.Now()

	err := superviseIndexer(context.Background(), pinger, func(context.Context) error {
		sessionCalls++
		return nil
	}, controller, reconnectPolicy{
		ProbeTimeout: 10 * time.Millisecond,
		RetryDelay:   time.Millisecond,
	}, logger)

	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(startedAt), 10*time.Millisecond)
	require.Equal(t, 2, calls)
	require.Equal(t, 1, sessionCalls)
}

func TestSuperviseIndexerStopsRetryDelayOnCancellation(t *testing.T) {
	_, store, controller := newTestReadinessController()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var sessionCalls atomic.Int32

	go func() {
		done <- superviseIndexer(ctx, pingerFunc(func(context.Context) error {
			return errors.New("unavailable")
		}), func(context.Context) error {
			sessionCalls.Add(1)
			return nil
		}, controller, reconnectPolicy{
			ProbeTimeout: time.Second,
			RetryDelay:   time.Hour,
		}, logger)
	}()

	require.Eventually(t, func() bool {
		return store.Snapshot().State == diagnostics.OperationalState_OPERATIONAL_STATE_RECONNECTING
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Zero(t, sessionCalls.Load())
}

func TestSuperviseIndexerDoesNotRetryFatalError(t *testing.T) {
	_, store, controller := newTestReadinessController()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fatalErr := errors.New("invalid notification payload")
	sessionCalls := 0

	err := superviseIndexer(context.Background(), pingerFunc(func(context.Context) error {
		return nil
	}), func(context.Context) error {
		sessionCalls++
		return fatalErr
	}, controller, reconnectPolicy{
		ProbeTimeout: time.Second,
		RetryDelay:   time.Millisecond,
	}, logger)

	require.ErrorIs(t, err, fatalErr)
	require.Equal(t, 1, sessionCalls)
	require.Equal(t, diagnostics.OperationalState_OPERATIONAL_STATE_FAILED, store.Snapshot().State)
	require.Equal(t, "indexer failed", store.Snapshot().LastError)
}

func TestSuperviseIndexerRetriesTypedNotificationFailure(t *testing.T) {
	_, store, controller := newTestReadinessController()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionCalls := 0

	err := superviseIndexer(context.Background(), pingerFunc(func(context.Context) error {
		return nil
	}), func(context.Context) error {
		sessionCalls++
		if sessionCalls == 1 {
			return apperrors.ErrNotificationConnectionLost
		}
		return nil
	}, controller, reconnectPolicy{
		ProbeTimeout: time.Second,
		RetryDelay:   time.Millisecond,
	}, logger)

	require.NoError(t, err)
	require.Equal(t, 2, sessionCalls)
	require.Equal(t, "postgres notification connection unavailable", store.Snapshot().LastError)
}

func TestSuperviseIndexerDoesNotLogPostgresConnectionDetails(t *testing.T) {
	_, _, controller := newTestReadinessController()
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	secretURI := "postgres://user:p%40ss@db.internal/archive?sslmode=disable"
	pingCalls := 0

	err := superviseIndexer(context.Background(), pingerFunc(func(context.Context) error {
		pingCalls++
		if pingCalls == 1 {
			return errors.New(secretURI)
		}
		return nil
	}), func(context.Context) error {
		return nil
	}, controller, reconnectPolicy{
		ProbeTimeout: time.Second,
		RetryDelay:   time.Millisecond,
	}, logger)

	require.NoError(t, err)
	require.Contains(t, output.String(), "postgres query connection unavailable")
	require.NotContains(t, output.String(), secretURI)
	require.NotContains(t, output.String(), "p%40ss")
	require.NotContains(t, output.String(), "db.internal")
}

func TestRunIndexerSessionBoundsNotificationConnectWithTimeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	connectTimeout := 10 * time.Millisecond
	startedAt := time.Now()
	connect := func(ctx context.Context, _ string) (postgresNotificationConn, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(connectTimeout), deadline, 5*time.Millisecond)
		<-ctx.Done()
		return nil, ctx.Err()
	}

	err := runIndexerSession(
		context.Background(),
		"postgres://unused",
		nil,
		nil,
		10,
		32,
		connectTimeout,
		connect,
		noopObserver{},
		logger,
		logger,
	)

	require.ErrorIs(t, err, apperrors.ErrNotificationConnectionLost)
	require.GreaterOrEqual(t, time.Since(startedAt), connectTimeout)
}

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

type noopObserver struct{}

func (noopObserver) OnSyncStarted()                       {}
func (noopObserver) OnSyncProgress(indexer.SyncProgress)  {}
func (noopObserver) OnSyncCompleted(indexer.SyncProgress) {}
