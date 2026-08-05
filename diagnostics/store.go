package diagnostics

import (
	"sync"
	"time"
)

// Snapshot is an immutable view of the wrapper's operational status.
type Snapshot struct {
	State                OperationalState
	Ready                bool
	Indexer              IndexerStatus
	StateSince           time.Time
	LastSuccessfulSyncAt *time.Time
	LastErrorAt          *time.Time
	LastError            string
}

// Store keeps the latest operational status in memory. It is safe for
// concurrent use.
type Store struct {
	mu       sync.RWMutex
	now      func() time.Time
	snapshot Snapshot
}

// NewStore creates a status store in the starting state.
func NewStore() *Store {
	return newStore(time.Now)
}

func newStore(now func() time.Time) *Store {
	startedAt := now().UTC()
	return &Store{
		now: now,
		snapshot: Snapshot{
			State:      StateStarting,
			StateSince: startedAt,
		},
	}
}

// SetState updates the operational state and readiness flag.
func (s *Store) SetState(state OperationalState, ready bool) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.State != state {
		s.snapshot.State = state
		s.snapshot.StateSince = s.now().UTC()
	}
	s.snapshot.Ready = ready
}

// SetProgress updates the latest indexing progress.
func (s *Store) SetProgress(initialized bool, indexedHeight, archiveHeight, targetHeight int64) {
	if s == nil {
		return
	}

	if !initialized {
		indexedHeight = 0
	}

	confirmedLag := int64(0)
	if initialized && targetHeight > indexedHeight {
		confirmedLag = targetHeight - indexedHeight
	}

	s.mu.Lock()
	s.snapshot.Indexer = IndexerStatus{
		Initialized:   initialized,
		IndexedHeight: indexedHeight,
		ArchiveHeight: archiveHeight,
		TargetHeight:  targetHeight,
		ConfirmedLag:  confirmedLag,
	}
	s.mu.Unlock()
}

// RecordSuccessfulSync records a completed reconciliation.
func (s *Store) RecordSuccessfulSync() {
	if s == nil {
		return
	}

	now := s.now().UTC()
	s.mu.Lock()
	s.snapshot.LastSuccessfulSyncAt = &now
	s.mu.Unlock()
}

// RecordError stores a safe, operator-facing error summary.
func (s *Store) RecordError(summary string) {
	if s == nil {
		return
	}

	now := s.now().UTC()
	s.mu.Lock()
	s.snapshot.LastError = summary
	s.snapshot.LastErrorAt = &now
	s.mu.Unlock()
}

// Snapshot returns a defensive copy of the latest status.
func (s *Store) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := s.snapshot
	snapshot.LastSuccessfulSyncAt = cloneTime(s.snapshot.LastSuccessfulSyncAt)
	snapshot.LastErrorAt = cloneTime(s.snapshot.LastErrorAt)
	return snapshot
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
