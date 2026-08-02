package main

import (
	"sync"

	"github.com/node101-io/archive-wrapper/diagnostics"
	"github.com/node101-io/archive-wrapper/indexer"
	grpcHealth "google.golang.org/grpc/health"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
)

// readinessController keeps gRPC health and diagnostics transitions consistent.
type readinessController struct {
	mu       sync.Mutex
	health   *grpcHealth.Server
	store    *diagnostics.Store
	terminal bool
}

// newReadinessController binds both readiness views to one transition owner.
func newReadinessController(health *grpcHealth.Server, store *diagnostics.Store) *readinessController {
	return &readinessController{health: health, store: store}
}

// Connecting marks query traffic unavailable while PostgreSQL is being probed.
func (c *readinessController) Connecting() {
	c.transitionUnavailable(diagnostics.OperationalState_OPERATIONAL_STATE_CONNECTING, "")
}

// Reconnecting records a safe connection summary and disables query readiness.
func (c *readinessController) Reconnecting(summary string) {
	c.transitionUnavailable(diagnostics.OperationalState_OPERATIONAL_STATE_RECONNECTING, summary)
}

// Failed records a non-serving runtime failure.
func (c *readinessController) Failed(summary string) {
	c.transitionUnavailable(diagnostics.OperationalState_OPERATIONAL_STATE_FAILED, summary)
}

// Stopping is terminal so late Indexer callbacks cannot restore readiness.
func (c *readinessController) Stopping() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal {
		return
	}
	c.terminal = true
	c.setQueryServingStatus(grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	c.store.SetState(diagnostics.OperationalState_OPERATIONAL_STATE_STOPPING, false)
}

// OnSyncStarted preserves readiness only for an already initialized incremental sync.
func (c *readinessController) OnSyncStarted() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal {
		return
	}

	snapshot := c.store.Snapshot()
	if !snapshot.Ready {
		c.setQueryServingStatus(grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	}
	c.store.SetState(diagnostics.OperationalState_OPERATIONAL_STATE_SYNCING, snapshot.Ready)
}

// OnSyncProgress refreshes diagnostics without changing serving status.
func (c *readinessController) OnSyncProgress(progress indexer.SyncProgress) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal {
		return
	}
	c.setProgress(progress)
}

// OnSyncCompleted publishes readiness only when a usable cursor exists.
func (c *readinessController) OnSyncCompleted(progress indexer.SyncProgress) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal {
		return
	}

	c.setProgress(progress)
	c.store.RecordSuccessfulSync()
	if !progress.Initialized {
		c.setQueryServingStatus(grpcHealthV1.HealthCheckResponse_NOT_SERVING)
		c.store.SetState(diagnostics.OperationalState_OPERATIONAL_STATE_WAITING_FOR_FINALITY, false)
		return
	}

	// Complete diagnostics first so a newly serving query never exposes stale status.
	c.store.SetState(diagnostics.OperationalState_OPERATIONAL_STATE_READY, true)
	c.setQueryServingStatus(grpcHealthV1.HealthCheckResponse_SERVING)
}

// transitionUnavailable updates health before diagnostics to fail closed.
func (c *readinessController) transitionUnavailable(state diagnostics.OperationalState, summary string) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal {
		return
	}

	c.setQueryServingStatus(grpcHealthV1.HealthCheckResponse_NOT_SERVING)
	if summary != "" {
		c.store.RecordError(summary)
	}
	c.store.SetState(state, false)
}

func (c *readinessController) setProgress(progress indexer.SyncProgress) {
	c.store.SetProgress(
		progress.Initialized,
		progress.IndexedHeight,
		progress.ArchiveHeight,
		progress.TargetHeight,
	)
}

func (c *readinessController) setQueryServingStatus(status grpcHealthV1.HealthCheckResponse_ServingStatus) {
	c.health.SetServingStatus("", status)
	c.health.SetServingStatus(queryGRPCServiceName, status)
}
