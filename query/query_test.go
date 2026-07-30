package query

import (
	"context"
	"io"
	"log/slog"
	"testing"

	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/database"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const blockHeightDatabaseKey = "db-key"
const testMaxBlockRange int64 = 1000

func TestQuery(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	want := actions.DbRecord{
		Key: 7,
		Actions: []*actions.Action{
			{
				BlockHeight: 7,
				FeePayer:    []byte("alice"),
				ActionType:  actions.ActionType_DEPOSIT,
				Amount:      42,
			},
		},
	}
	second := actions.DbRecord{
		Key: 9,
		Actions: []*actions.Action{
			{
				BlockHeight: 9,
				FeePayer:    []byte("bob"),
				ActionType:  actions.ActionType_WITHDRAW,
				Amount:      7,
			},
		},
	}

	err = manager.Insert(want)
	require.NoError(t, err)
	err = manager.Insert(second)
	require.NoError(t, err)

	err = manager.EnsureStartBlockHeight(want.Key)
	require.NoError(t, err)

	err = manager.InsertBlockHeight(second.Key)
	require.NoError(t, err)

	q, err := NewQuery(manager, slog.New(slog.NewTextHandler(io.Discard, nil)), testMaxBlockRange)
	require.NoError(t, err)
	got, err := q.GetActionsInRange(context.Background(), &QueryGetActionsInRangeRequest{
		StartBlockHeight: 7,
		EndBlockHeight:   9,
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Actions, 2)

	require.Equal(t, want.Actions[0].BlockHeight, got.Actions[0].BlockHeight)
	require.Equal(t, want.Actions[0].FeePayer, got.Actions[0].FeePayer)
	require.Equal(t, want.Actions[0].ActionType, got.Actions[0].ActionType)
	require.Equal(t, want.Actions[0].Amount, got.Actions[0].Amount)

	require.Equal(t, second.Actions[0].BlockHeight, got.Actions[1].BlockHeight)
	require.Equal(t, second.Actions[0].FeePayer, got.Actions[1].FeePayer)
	require.Equal(t, second.Actions[0].ActionType, got.Actions[1].ActionType)
	require.Equal(t, second.Actions[0].Amount, got.Actions[1].Amount)
}

func TestQuery_ProcessedEmptyBlockReturnsEmptyList(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	err = manager.EnsureStartBlockHeight(7)
	require.NoError(t, err)

	err = manager.InsertBlockHeight(7)
	require.NoError(t, err)

	q, err := NewQuery(manager, logger, testMaxBlockRange)
	require.NoError(t, err)

	got, err := q.GetActionsInRange(context.Background(), &QueryGetActionsInRangeRequest{
		StartBlockHeight: 7,
		EndBlockHeight:   7,
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got.Actions)
}

func TestQuery_NoProcessedBlocksReturnsFailedPrecondition(t *testing.T) {
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

	got, err := q.GetActionsInRange(context.Background(), &QueryGetActionsInRangeRequest{
		StartBlockHeight: 7,
		EndBlockHeight:   7,
	})

	require.Nil(t, got)
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "indexer has not processed any blocks yet", status.Convert(err).Message())
}

func TestQuery_RangeBelowStartHeightReturnsFailedPrecondition(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	err = manager.EnsureStartBlockHeight(7)
	require.NoError(t, err)
	err = manager.InsertBlockHeight(9)
	require.NoError(t, err)

	q, err := NewQuery(manager, logger, testMaxBlockRange)
	require.NoError(t, err)

	got, err := q.GetActionsInRange(context.Background(), &QueryGetActionsInRangeRequest{
		StartBlockHeight: 6,
		EndBlockHeight:   9,
	})

	require.Nil(t, got)
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "start block height 6 is lower than earliest indexed block 7", status.Convert(err).Message())
}

func TestQuery_RangeAboveMaximumWidthReturnsInvalidArgument(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	err = manager.EnsureStartBlockHeight(1)
	require.NoError(t, err)
	err = manager.InsertBlockHeight(testMaxBlockRange + 1)
	require.NoError(t, err)

	q, err := NewQuery(manager, logger, testMaxBlockRange)
	require.NoError(t, err)

	got, err := q.GetActionsInRange(context.Background(), &QueryGetActionsInRangeRequest{
		StartBlockHeight: 1,
		EndBlockHeight:   testMaxBlockRange + 1,
	})

	require.Nil(t, got)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "requested block range exceeds maximum width of 1000 heights", status.Convert(err).Message())
}

func TestQuery_CanceledContextStopsRangeScan(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	err = manager.EnsureStartBlockHeight(7)
	require.NoError(t, err)
	err = manager.InsertBlockHeight(7)
	require.NoError(t, err)

	q, err := NewQuery(manager, logger, testMaxBlockRange)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := q.GetActionsInRange(ctx, &QueryGetActionsInRangeRequest{
		StartBlockHeight: 7,
		EndBlockHeight:   7,
	})

	require.Nil(t, got)
	require.Error(t, err)
	require.Equal(t, codes.Canceled, status.Code(err))
}
