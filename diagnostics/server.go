package diagnostics

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server exposes the wrapper's current operational status.
type Server struct {
	store *Store
}

// NewServer creates a diagnostics server backed by store.
func NewServer(store *Store) *Server {
	return &Server{store: store}
}

// GetStatus returns the latest in-memory status snapshot.
func (s *Server) GetStatus(_ context.Context, request *GetStatusRequest) (*GetStatusResponse, error) {
	if s == nil || s.store == nil {
		return nil, status.Error(codes.FailedPrecondition, "diagnostics service is not initialized")
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	snapshot := s.store.Snapshot()
	return &GetStatusResponse{
		State:                snapshot.State,
		Ready:                snapshot.Ready,
		Indexer:              snapshot.Indexer,
		StateSince:           cloneTime(&snapshot.StateSince),
		LastSuccessfulSyncAt: cloneTime(snapshot.LastSuccessfulSyncAt),
		LastErrorAt:          cloneTime(snapshot.LastErrorAt),
		LastError:            snapshot.LastError,
	}, nil
}
