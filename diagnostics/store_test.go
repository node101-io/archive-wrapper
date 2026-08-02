package diagnostics

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStoreTracksStateProgressAndHistory(t *testing.T) {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	store := newStore(func() time.Time { return now })

	initial := store.Snapshot()
	require.Equal(t, OperationalState_OPERATIONAL_STATE_STARTING, initial.State)
	require.False(t, initial.Ready)
	require.Equal(t, now, initial.StateSince)

	now = now.Add(time.Second)
	store.SetState(OperationalState_OPERATIONAL_STATE_STARTING, false)
	require.Equal(t, initial.StateSince, store.Snapshot().StateSince)

	now = now.Add(time.Second)
	store.SetState(OperationalState_OPERATIONAL_STATE_SYNCING, false)
	store.SetProgress(true, 10, 45, 13)
	store.RecordSuccessfulSync()
	store.RecordError("postgres query connection unavailable")

	snapshot := store.Snapshot()
	require.Equal(t, OperationalState_OPERATIONAL_STATE_SYNCING, snapshot.State)
	require.Equal(t, now, snapshot.StateSince)
	require.Equal(t, IndexerStatus{
		Initialized:   true,
		IndexedHeight: 10,
		ArchiveHeight: 45,
		TargetHeight:  13,
		ConfirmedLag:  3,
	}, snapshot.Indexer)
	require.Equal(t, now, *snapshot.LastSuccessfulSyncAt)
	require.Equal(t, now, *snapshot.LastErrorAt)
	require.Equal(t, "postgres query connection unavailable", snapshot.LastError)

	*snapshot.LastSuccessfulSyncAt = snapshot.LastSuccessfulSyncAt.Add(time.Hour)
	require.Equal(t, now, *store.Snapshot().LastSuccessfulSyncAt)
}

func TestStoreUsesZeroLagWithoutInitializedCursor(t *testing.T) {
	store := NewStore()
	store.SetProgress(false, 999, 20, 10)

	status := store.Snapshot().Indexer
	require.False(t, status.Initialized)
	require.Zero(t, status.IndexedHeight)
	require.Zero(t, status.ConfirmedLag)
}

func TestServerGetStatusReturnsDefensiveSnapshot(t *testing.T) {
	store := NewStore()
	store.SetProgress(true, 10, 45, 13)
	server := NewServer(store)

	response, err := server.GetStatus(context.Background(), &GetStatusRequest{})
	require.NoError(t, err)
	require.Equal(t, int64(3), response.Indexer.ConfirmedLag)
	require.NotNil(t, response.StateSince)

	response.Indexer.IndexedHeight = 999
	second, err := server.GetStatus(context.Background(), &GetStatusRequest{})
	require.NoError(t, err)
	require.Equal(t, int64(10), second.Indexer.IndexedHeight)
}

func TestServerGetStatusValidatesDependenciesAndRequest(t *testing.T) {
	response, err := (*Server)(nil).GetStatus(context.Background(), &GetStatusRequest{})
	require.Nil(t, response)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	response, err = NewServer(nil).GetStatus(context.Background(), &GetStatusRequest{})
	require.Nil(t, response)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	response, err = NewServer(NewStore()).GetStatus(context.Background(), nil)
	require.Nil(t, response)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestStoreSupportsConcurrentReadersAndWriters(t *testing.T) {
	store := NewStore()
	var group sync.WaitGroup

	for i := 0; i < 50; i++ {
		group.Add(2)
		go func(height int64) {
			defer group.Done()
			store.SetProgress(true, height, height+32, height)
			store.SetState(OperationalState_OPERATIONAL_STATE_SYNCING, true)
		}(int64(i + 1))
		go func() {
			defer group.Done()
			_ = store.Snapshot()
		}()
	}

	group.Wait()
	require.True(t, store.Snapshot().Indexer.Initialized)
}
