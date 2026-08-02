package main

import (
	"sync"

	"github.com/node101-io/archive-wrapper/diagnostics"
	"github.com/node101-io/archive-wrapper/indexer"
	grpcHealth "google.golang.org/grpc/health"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
)

type readinessController struct {
	mu       sync.Mutex
	health   *grpcHealth.Server
	store    *diagnostics.Store
	terminal bool
}

func newReadinessController(health *grpcHealth.Server, store *diagnostics.Store) *readinessController {
	return &readinessController{health: health, store: store}
}

func (c *readinessController) Connecting() {
	c.transitionUnavailable(diagnostics.OperationalState_OPERATIONAL_STATE_CONNECTING, "")
}

func (c *readinessController) Reconnecting(summary string) {
	c.transitionUnavailable(diagnostics.OperationalState_OPERATIONAL_STATE_RECONNECTING, summary)
}

func (c *readinessController) Failed(summary string) {
	c.transitionUnavailable(diagnostics.OperationalState_OPERATIONAL_STATE_FAILED, summary)
}

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

	c.store.SetState(diagnostics.OperationalState_OPERATIONAL_STATE_READY, true)
	c.setQueryServingStatus(grpcHealthV1.HealthCheckResponse_SERVING)
}

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
