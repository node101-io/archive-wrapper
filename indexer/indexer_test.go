package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	fetchmina "github.com/node101-io/archive-wrapper/fetchmina"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/stretchr/testify/require"
)

const blockHeightDatabaseKey = "db-key"
const testContractAddress = "B62qjTpSX2R4fyqJrC9pzvm5PSZb5GMaYBXh8di3TBn6pPC9XXYFC9k"

func TestNewIndexerRejectsNilConnection(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := withRetry(ctx, logger, "test operation", func() error {
		return errors.New("boom")
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestRunReconcilesMissingHeightsFromNotificationAndSkipsDuplicateOrOutOfOrderNotifications(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	conn := &fakeNotificationConn{
		notifications: []*pgconn.Notification{
			mustNotification(t, BlockNotification{Height: 102}),
			mustNotification(t, BlockNotification{Height: 102}),
			mustNotification(t, BlockNotification{Height: 101}),
		},
	}

	querier := &fakeQuerier{
		conn:         conn,
		latestHeight: 100,
		rowsByHeight: map[int64][]sqlcdb.ListActionRowsRow{
			69: {validActionRow(69)},
			70: {validActionRow(70)},
		},
	}

	client, err := fetchmina.NewMinaClient(testContractAddress, querier, logger)
	require.NoError(t, err)

	db, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, db.Close())
	}()

	require.NoError(t, db.InsertBlockHeight(68))

	conn.beforeWait = func(waitCalls int) {
		if waitCalls != 0 {
			return
		}

		cursor, err := db.GetBlockHeight()
		require.NoError(t, err)
		require.Equal(t, int64(68), cursor)
	}

	indexer, err := NewIndexer(conn, client, db, 10, 32, logger)
	require.NoError(t, err)

	require.NoError(t, indexer.Run(context.Background()))

	require.Equal(t, []string{"LISTEN blocks_inserted"}, conn.execStatements)
	require.Equal(t, 3, conn.waitCalls)
	require.Equal(t, []heightRequest{
		{height: 69, waitCalls: 1},
		{height: 70, waitCalls: 1},
	}, querier.requestedHeights)

	cursor, err := db.GetBlockHeight()
	require.NoError(t, err)
	require.Equal(t, int64(70), cursor)
}

func TestIndexAvailableBlocksDoesNotAdvanceCursorOnInvalidBlock(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	conn := &fakeNotificationConn{}
	querier := &fakeQuerier{
		conn: conn,
		rowsByHeight: map[int64][]sqlcdb.ListActionRowsRow{
			69: {validActionRow(70)},
		},
	}

	client, err := fetchmina.NewMinaClient(testContractAddress, querier, logger)
	require.NoError(t, err)

	db, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, db.Close())
	}()

	require.NoError(t, db.InsertBlockHeight(68))

	indexer, err := NewIndexer(conn, client, db, 10, 32, logger)
	require.NoError(t, err)

	err = indexer.indexAvailableBlocks(context.Background(), 69)
	require.ErrorIs(t, err, apperrors.ErrInvalidBlockHeight)

	cursor, err := db.GetBlockHeight()
	require.NoError(t, err)
	require.Equal(t, int64(68), cursor)

	hasRecord, err := db.Has(69)
	require.NoError(t, err)
	require.False(t, hasRecord)
}

type fakeNotificationConn struct {
	execStatements []string
	notifications  []*pgconn.Notification
	waitCalls      int
	listenReady    bool
	beforeWait     func(waitCalls int)
}

func (c *fakeNotificationConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	c.execStatements = append(c.execStatements, sql)
	if sql == "LISTEN blocks_inserted" {
		c.listenReady = true
	}

	return pgconn.CommandTag{}, nil
}

func (c *fakeNotificationConn) WaitForNotification(_ context.Context) (*pgconn.Notification, error) {
	if c.beforeWait != nil {
		c.beforeWait(c.waitCalls)
	}

	if c.waitCalls >= len(c.notifications) {
		return nil, context.Canceled
	}

	notification := c.notifications[c.waitCalls]
	c.waitCalls++

	return notification, nil
}

type heightRequest struct {
	height    int64
	waitCalls int
}

type fakeQuerier struct {
	conn             *fakeNotificationConn
	latestHeight     int64
	requestedHeights []heightRequest
	rowsByHeight     map[int64][]sqlcdb.ListActionRowsRow
}

func (q *fakeQuerier) GetLatestBlockHeight(context.Context) (int64, error) {
	if !q.conn.listenReady {
		return 0, errors.New("LISTEN must be registered before initial sync")
	}

	return q.latestHeight, nil
}

func (q *fakeQuerier) ListActionRows(_ context.Context, arg sqlcdb.ListActionRowsParams) ([]sqlcdb.ListActionRowsRow, error) {
	q.requestedHeights = append(q.requestedHeights, heightRequest{
		height:    arg.Height,
		waitCalls: q.conn.waitCalls,
	})

	return q.rowsByHeight[arg.Height], nil
}

func mustNotification(t *testing.T, msg BlockNotification) *pgconn.Notification {
	t.Helper()

	payload, err := json.Marshal(msg)
	require.NoError(t, err)

	return &pgconn.Notification{Payload: string(payload)}
}

func validActionRow(height int64) sqlcdb.ListActionRowsRow {
	return sqlcdb.ListActionRowsRow{
		Height:   height,
		FeePayer: testContractAddress,
		Data:     []string{"1", "ignored", "ignored", "42"},
	}
}
