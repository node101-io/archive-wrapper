package query

import (
	"context"
	"io"
	"log/slog"
	"testing"

	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/database"

	"github.com/stretchr/testify/require"
)

const blockHeightDatabaseKey = "db-key"

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

	err = manager.InsertBlockHeight(second.Key)
	require.NoError(t, err)

	q, err := NewQuery(manager, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

	err = manager.InsertBlockHeight(7)
	require.NoError(t, err)

	q, err := NewQuery(manager, logger)
	require.NoError(t, err)

	got, err := q.GetActionsInRange(context.Background(), &QueryGetActionsInRangeRequest{
		StartBlockHeight: 7,
		EndBlockHeight:   7,
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got.Actions)
}
