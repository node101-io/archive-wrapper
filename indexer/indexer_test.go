package indexer

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	fetchmina "github.com/node101-io/archive-wrapper/fetchmina"
	"github.com/stretchr/testify/require"
)

const blockHeightDatabaseKey = "db-key"

func TestNewIndexerRejectsNilConnection(t *testing.T) {

	logger := slog.Default()
	require.NotNil(t, logger)

	db, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, db.Close())
	}()

	indexer, err := NewIndexer(nil, &fetchmina.MinaClient{}, db, 10, 32, logger)

	require.Nil(t, indexer)
	require.ErrorIs(t, err, apperrors.ErrNilConnection)
}

func TestIndexActionsBuildsRecord(t *testing.T) {
	items := []actions.Action{
		{BlockHeight: 7, FeePayer: []byte("alice"), ActionType: actions.ActionType_DEPOSIT, Amount: 5},
		{BlockHeight: 7, FeePayer: []byte("bob"), ActionType: actions.ActionType_WITHDRAW, Amount: 3},
	}

	record, err := IndexActions(items, 7)
	require.NoError(t, err)

	require.Equal(t, int64(7), record.Key)
	require.Len(t, record.Actions, 2)
	require.Equal(t, []byte("alice"), record.Actions[0].FeePayer)
	require.Equal(t, []byte("bob"), record.Actions[1].FeePayer)
}

func TestWithRetryReturnsContextErrorWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	logger := slog.Default()
	err := withRetry(ctx, logger, "test operation", func() error {
		return errors.New("boom")
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
}
