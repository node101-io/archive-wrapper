package query

import (
	"context"
	io "io"
	"log/slog"
	"testing"

	"github.com/node101-io/archive-wrapper/database"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestQuery_GetMinaBlockHeight(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	err = manager.InsertBlockHeight(7)
	require.NoError(t, err)

	q, err := NewQuery(manager, slog.New(slog.NewTextHandler(io.Discard, nil)), testMaxBlockRange)
	require.NoError(t, err)

	got, err := q.GetMinaBlockHeight(context.Background(), &QueryGetMinaBlockHeightRequest{})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(7), got.BlockHeight)
}

func TestQuery_GetMinaBlockHeight_NoProcessedBlocksReturnsFailedPrecondition(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	q, err := NewQuery(manager, logger, testMaxBlockRange)
	require.NoError(t, err)

	got, err := q.GetMinaBlockHeight(context.Background(), &QueryGetMinaBlockHeightRequest{})
	require.Nil(t, got)
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "indexer has not processed any blocks yet", status.Convert(err).Message())
}
