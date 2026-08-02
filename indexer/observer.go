package indexer

// SyncProgress describes the archive and local cursor positions for a sync.
type SyncProgress struct {
	ArchiveHeight int64
	TargetHeight  int64
	Initialized   bool
	IndexedHeight int64
}

// SyncObserver receives synchronous lifecycle updates on the indexer run
// goroutine. Implementations must return promptly and must not call back into
// the same Indexer.
type SyncObserver interface {
	OnSyncStarted()
	OnSyncProgress(SyncProgress)
	OnSyncCompleted(SyncProgress)
}

// Option configures an Indexer.
type Option func(*Indexer)

// WithSyncObserver reports indexing progress to observer. A nil observer is
// ignored; the default observer performs no work.
func WithSyncObserver(observer SyncObserver) Option {
	return func(indexer *Indexer) {
		if observer != nil {
			indexer.observer = observer
		}
	}
}

type noopSyncObserver struct{}

func (noopSyncObserver) OnSyncStarted()               {}
func (noopSyncObserver) OnSyncProgress(SyncProgress)  {}
func (noopSyncObserver) OnSyncCompleted(SyncProgress) {}
