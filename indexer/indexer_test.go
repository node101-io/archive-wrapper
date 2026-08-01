package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
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
		blockIDsByHeight: map[int64]int64{
			69: 690,
			70: 700,
		},
		rowsByHeight: map[int64][]sqlcdb.ListActionRowsByBlockIDRow{
			69: {validActionRow(69)},
			70: {validActionRow(70)},
		},
	}

	client, err := fetchmina.NewMinaClient(testContractAddress, querier, logger)
	require.NoError(t, err)

	dbPath := t.TempDir()
	db, err := database.NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NoError(t, db.CommitBlock(actions.DbRecord{
		Key: 68,
		Actions: []*actions.Action{{
			BlockHeight: 68,
			FeePayer:    []byte(testContractAddress),
			ActionType:  actions.ActionType_DEPOSIT,
			Amount:      42,
		}},
	}))
	// Reopen the DB to verify restart resumes after the atomically committed block.
	require.NoError(t, db.Close())

	db, err = database.NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, db.Close())
	}()

	hasRecord, err := db.Has(68)
	require.NoError(t, err)
	require.True(t, hasRecord)

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
	require.Equal(t, []rangeRequest{
		{startHeight: 69, endHeight: 70, waitCalls: 1},
	}, querier.primedRanges)
	require.Equal(t, []heightRequest{
		{height: 69, waitCalls: 1},
		{height: 70, waitCalls: 1},
	}, querier.requestedActionHeights)
	require.Empty(t, querier.pointLookupHeights)

	cursor, err := db.GetBlockHeight()
	require.NoError(t, err)
	require.Equal(t, int64(70), cursor)
}

func TestSyncToAfterCursorlessRestartDoesNotStoreEmptyBlock(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := t.TempDir()

	db, err := database.NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	// Persisted deployment metadata represents initialization before the first cursor.
	require.NoError(t, db.EnsureDeploymentMetadata(
		"archive-wrapper:deployment",
		database.DeploymentMetadata{
			SchemaVersion:   1,
			MinaNetworkID:   "testnet",
			ContractAddress: testContractAddress,
			StartHeight:     10,
		},
	))
	// Reopen after initialization to simulate a restart before the first cursor.
	require.NoError(t, db.Close())

	db, err = database.NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, db.Close())
	}()

	conn := &fakeNotificationConn{}
	querier := &fakeQuerier{
		conn: conn,
		blockIDsByHeight: map[int64]int64{
			10: 100,
		},
		rowsByHeight: map[int64][]sqlcdb.ListActionRowsByBlockIDRow{
			10: {},
		},
	}
	client, err := fetchmina.NewMinaClient(testContractAddress, querier, logger)
	require.NoError(t, err)

	indexer, err := NewIndexer(conn, client, db, 10, 32, logger)
	require.NoError(t, err)
	require.NoError(t, indexer.syncTo(context.Background(), 10))

	hasRecord, err := db.Has(10)
	require.NoError(t, err)
	require.False(t, hasRecord)
	cursor, err := db.GetBlockHeight()
	require.NoError(t, err)
	require.Equal(t, int64(10), cursor)
}

func TestIndexAvailableBlocksDoesNotAdvanceCursorOnInvalidBlock(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	conn := &fakeNotificationConn{}
	querier := &fakeQuerier{
		conn: conn,
		blockIDsByHeight: map[int64]int64{
			69: 690,
		},
		rowsByHeight: map[int64][]sqlcdb.ListActionRowsByBlockIDRow{
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

func TestIndexAvailableBlocksDoesNotAdvanceCursorWhenBestChainBlockMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	conn := &fakeNotificationConn{}
	querier := &fakeQuerier{
		conn:             conn,
		blockIDsByHeight: map[int64]int64{},
		rowsByHeight:     map[int64][]sqlcdb.ListActionRowsByBlockIDRow{},
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
	require.ErrorIs(t, err, apperrors.ErrBestChainBlockNotFound)

	cursor, err := db.GetBlockHeight()
	require.NoError(t, err)
	require.Equal(t, int64(68), cursor)

	hasRecord, err := db.Has(69)
	require.NoError(t, err)
	require.False(t, hasRecord)
}

func TestRunReturnsReconnectableErrorWhenInitialSyncQueryConnectionFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	conn := &fakeNotificationConn{}
	querier := &fakeQuerier{
		conn:            conn,
		latestHeightErr: context.DeadlineExceeded,
	}

	client, err := fetchmina.NewMinaClient(testContractAddress, querier, logger)
	require.NoError(t, err)

	db, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, db.Close())
	}()

	indexer, err := NewIndexer(conn, client, db, 10, 32, logger)
	require.NoError(t, err)

	err = indexer.Run(context.Background())
	require.ErrorIs(t, err, apperrors.ErrQueryConnectionLost)
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

type rangeRequest struct {
	startHeight int64
	endHeight   int64
	waitCalls   int
}

type fakeQuerier struct {
	conn                   *fakeNotificationConn
	latestHeight           int64
	latestHeightErr        error
	blockIDsByHeight       map[int64]int64
	primedRanges           []rangeRequest
	requestedActionHeights []heightRequest
	pointLookupHeights     []int64
	rowsByHeight           map[int64][]sqlcdb.ListActionRowsByBlockIDRow
}

func (q *fakeQuerier) GetLatestBlockHeight(context.Context) (int64, error) {
	if !q.conn.listenReady {
		return 0, errors.New("LISTEN must be registered before initial sync")
	}
	if q.latestHeightErr != nil {
		return 0, q.latestHeightErr
	}

	return q.latestHeight, nil
}

func (q *fakeQuerier) GetBestChainBlockIDAtHeight(_ context.Context, height int64) (sqlcdb.GetBestChainBlockIDAtHeightRow, error) {
	q.pointLookupHeights = append(q.pointLookupHeights, height)

	blockID, ok := q.blockIDsByHeight[height]
	if !ok {
		return sqlcdb.GetBestChainBlockIDAtHeightRow{}, pgx.ErrNoRows
	}

	return sqlcdb.GetBestChainBlockIDAtHeightRow{
		ID:     blockID,
		Height: height,
	}, nil
}

func (q *fakeQuerier) ListBestChainBlockIDsInRange(_ context.Context, arg sqlcdb.ListBestChainBlockIDsInRangeParams) ([]sqlcdb.ListBestChainBlockIDsInRangeRow, error) {
	q.primedRanges = append(q.primedRanges, rangeRequest{
		startHeight: arg.StartHeight,
		endHeight:   arg.EndHeight,
		waitCalls:   q.conn.waitCalls,
	})

	rows := make([]sqlcdb.ListBestChainBlockIDsInRangeRow, 0, arg.EndHeight-arg.StartHeight+1)
	for height := arg.StartHeight; height <= arg.EndHeight; height++ {
		blockID, ok := q.blockIDsByHeight[height]
		if !ok {
			continue
		}
		rows = append(rows, sqlcdb.ListBestChainBlockIDsInRangeRow{
			ID:     blockID,
			Height: height,
		})
	}

	return rows, nil
}

func (q *fakeQuerier) ListActionRowsByBlockID(_ context.Context, arg sqlcdb.ListActionRowsByBlockIDParams) ([]sqlcdb.ListActionRowsByBlockIDRow, error) {
	height := int64(0)
	for candidateHeight, candidateBlockID := range q.blockIDsByHeight {
		if candidateBlockID == arg.BlockID {
			height = candidateHeight
			break
		}
	}

	q.requestedActionHeights = append(q.requestedActionHeights, heightRequest{
		height:    height,
		waitCalls: q.conn.waitCalls,
	})

	return q.rowsByHeight[height], nil
}

func mustNotification(t *testing.T, msg BlockNotification) *pgconn.Notification {
	t.Helper()

	payload, err := json.Marshal(msg)
	require.NoError(t, err)

	return &pgconn.Notification{Payload: string(payload)}
}

func validActionRow(height int64) sqlcdb.ListActionRowsByBlockIDRow {
	return sqlcdb.ListActionRowsByBlockIDRow{
		Height:   height,
		FeePayer: testContractAddress,
		Data:     []string{"1", "ignored", "ignored", "42"},
	}
}
