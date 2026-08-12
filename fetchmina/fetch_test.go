package fetchmina

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/stretchr/testify/require"
)

const validAddress = "B62qjTpSX2R4fyqJrC9pzvm5PSZb5GMaYBXh8di3TBn6pPC9XXYFC9k"

const (
	pallasBaseFieldModulus         = "28948022309329048855892746252171976963363056481941560715954676764349967630337"
	pallasBaseFieldModulusMinusOne = "28948022309329048855892746252171976963363056481941560715954676764349967630336"
	integerAboveFieldByteSize      = "115792089237316195423570985008687907853269984665640564039457584007913129639936"
)

func TestNewMinaClientRejectsNilQueries(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	client, err := NewMinaClient(validAddress, nil, logger)

	require.Nil(t, client)
	require.Error(t, err)
	require.True(t, errors.Is(err, apperrors.ErrNilQueries))
}

func TestNewMinaClientRejectsInvalidContractAddress(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	client, err := NewMinaClient("not-an-address", &sqlcdb.Queries{}, logger)
	require.Nil(t, client)
	require.Error(t, err)
}

func TestActionFromRawDataBuildsAction(t *testing.T) {
	tests := []struct {
		name       string
		data       []string
		actionType actions.ActionType
		isOdd      bool
	}{
		{name: "deposit", data: []string{"1", "7", "1", "42"}, actionType: actions.ActionType_DEPOSIT, isOdd: true},
		{name: "withdraw", data: []string{"2", "7", "0", "42"}, actionType: actions.ActionType_WITHDRAW, isOdd: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := actionFromRawData(99, tt.data)
			require.NoError(t, err)
			require.NotNil(t, action)

			require.Equal(t, int64(99), action.BlockHeight)
			require.Equal(t, tt.actionType, action.ActionType)
			require.Equal(t, int64(42), action.Amount)
			require.Equal(t, append(make([]byte, 31), 7), action.XCoordinate)
			require.Equal(t, tt.isOdd, action.IsOdd)
		})
	}
}

func TestActionFromRawDataRejectsIncompletePayloads(t *testing.T) {
	tests := []struct {
		name string
		data []string
	}{
		{name: "one field", data: []string{"1"}},
		{name: "two fields", data: []string{"1", "7"}},
		{name: "three fields", data: []string{"1", "7", "1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var action *actions.Action
			var err error

			require.NotPanics(t, func() {
				action, err = actionFromRawData(99, tt.data)
			})
			require.Nil(t, action)
			require.ErrorIs(t, err, apperrors.ErrInvalidActionData)
		})
	}
}

func TestFieldBytesFromDecimal(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "zero", value: "0"},
		{name: "small value", value: "7"},
		{name: "largest canonical value", value: pallasBaseFieldModulusMinusOne},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fieldBytesFromDecimal(tt.value)
			require.NoError(t, err)
			require.Len(t, got, 32)
		})
	}
}

func TestFieldBytesFromDecimalRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "malformed", value: "not-a-number"},
		{name: "negative", value: "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fieldBytesFromDecimal(tt.value)

			require.Nil(t, got)
			require.ErrorIs(t, err, apperrors.ErrInvalidActionData)
		})
	}
}

func TestFieldBytesFromDecimalForwardsNonCanonicalValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "field modulus", value: pallasBaseFieldModulus},
		{name: "above field byte size", value: integerAboveFieldByteSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fieldBytesFromDecimal(tt.value)

			require.NoError(t, err)
			require.Equal(t, tt.value, new(big.Int).SetBytes(got).String())
		})
	}
}

func TestParseIsOddField(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "even", value: "0", want: false},
		{name: "odd", value: "1", want: true},
		{name: "contract-invalid value defaults to even", value: "2", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIsOddField(tt.value)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestActionFromRawDataDefersContractInvariantValidation(t *testing.T) {
	tests := []struct {
		name           string
		data           []string
		wantActionType actions.ActionType
		wantAmount     int64
		wantIsOdd      bool
	}{
		{
			name:           "unspecified action type",
			data:           []string{"0", "7", "0", "42"},
			wantActionType: actions.ActionType_UNSPECIFIED,
			wantAmount:     42,
		},
		{
			name:           "unknown action type",
			data:           []string{"3", "7", "1", "42"},
			wantActionType: actions.ActionType(3),
			wantAmount:     42,
			wantIsOdd:      true,
		},
		{
			name:           "zero amount",
			data:           []string{"1", "7", "1", "0"},
			wantActionType: actions.ActionType_DEPOSIT,
			wantAmount:     0,
			wantIsOdd:      true,
		},
		{
			name:           "negative amount",
			data:           []string{"2", "7", "0", "-1"},
			wantActionType: actions.ActionType_WITHDRAW,
			wantAmount:     -1,
		},
		{
			name:           "contract-invalid parity defaults to even",
			data:           []string{"1", "7", "2", "42"},
			wantActionType: actions.ActionType_DEPOSIT,
			wantAmount:     42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := actionFromRawData(99, tt.data)

			require.NoError(t, err)
			require.NotNil(t, action)
			require.Equal(t, tt.wantActionType, action.ActionType)
			require.Equal(t, tt.wantAmount, action.Amount)
			require.Equal(t, tt.wantIsOdd, action.IsOdd)
		})
	}
}

func TestActionFromRawDataForwardsNonCanonicalXCoordinate(t *testing.T) {
	action, err := actionFromRawData(99, []string{"1", pallasBaseFieldModulus, "1", "42"})

	require.NoError(t, err)
	require.NotNil(t, action)
	require.Equal(t, pallasBaseFieldModulus, new(big.Int).SetBytes(action.XCoordinate).String())
}

func TestGetMinaBlockHeightWrapsRetryableQueryErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	querier := &trackingQuerier{
		latestHeightErr: context.DeadlineExceeded,
	}

	client, err := NewMinaClient(validAddress, querier, logger)
	require.NoError(t, err)

	height, err := client.GetMinaBlockHeight(context.Background())
	require.Zero(t, height)
	require.ErrorIs(t, err, apperrors.ErrQueryConnectionLost)
}

func TestWrapQueryErrorClassifiesClosedConnections(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "direct", err: pgconn.ErrConnClosed},
		{name: "wrapped", err: fmt.Errorf("query failed: %w", pgconn.ErrConnClosed)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapQueryError("query actions", tt.err)

			require.ErrorIs(t, got, apperrors.ErrQueryConnectionLost)
			require.ErrorIs(t, got, pgconn.ErrConnClosed)
		})
	}
}

func TestPrimeBestChainRangeCachesBlockIDsForFetchActions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	querier := &trackingQuerier{
		bestChainRowsByRange: map[rangeKey][]sqlcdb.ListBestChainBlockIDsInRangeRow{
			{startHeight: 90, endHeight: 91}: {
				{ID: 900, Height: 90},
				{ID: 901, Height: 91},
			},
		},
		bestChainBlockIDsByHeight: map[int64]int64{
			90: 900,
			91: 901,
		},
		actionRowsByBlockID: map[int64][]sqlcdb.ListActionRowsByBlockIDRow{
			900: {validActionQueryRow(90)},
			901: {validActionQueryRow(91)},
		},
	}

	client, err := NewMinaClient(validAddress, querier, logger)
	require.NoError(t, err)

	require.NoError(t, client.PrimeBestChainRange(context.Background(), 90, 91))

	actions90, err := client.FetchActions(context.Background(), 90)
	require.NoError(t, err)
	require.Len(t, actions90, 1)
	require.Equal(t, int64(90), actions90[0].BlockHeight)

	actions91, err := client.FetchActions(context.Background(), 91)
	require.NoError(t, err)
	require.Len(t, actions91, 1)
	require.Equal(t, int64(91), actions91[0].BlockHeight)

	require.Equal(t, []rangeKey{{startHeight: 90, endHeight: 91}}, querier.primedRanges)
	require.Empty(t, querier.pointLookupHeights)
	require.Equal(t, []int64{900, 901}, querier.actionBlockIDs)
}

func TestFetchActionsFallsBackToPointLookupOncePerHeight(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	querier := &trackingQuerier{
		bestChainBlockIDsByHeight: map[int64]int64{
			90: 900,
		},
		actionRowsByBlockID: map[int64][]sqlcdb.ListActionRowsByBlockIDRow{
			900: {validActionQueryRow(90)},
		},
	}

	client, err := NewMinaClient(validAddress, querier, logger)
	require.NoError(t, err)

	firstFetch, err := client.FetchActions(context.Background(), 90)
	require.NoError(t, err)
	require.Len(t, firstFetch, 1)

	secondFetch, err := client.FetchActions(context.Background(), 90)
	require.NoError(t, err)
	require.Len(t, secondFetch, 1)

	require.Equal(t, []int64{90}, querier.pointLookupHeights)
	require.Equal(t, []int64{900, 900}, querier.actionBlockIDs)
}

func TestFetchActionsReturnsErrorWhenBestChainBlockMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	querier := &trackingQuerier{
		bestChainBlockIDsByHeight: map[int64]int64{},
		actionRowsByBlockID:       map[int64][]sqlcdb.ListActionRowsByBlockIDRow{},
	}

	client, err := NewMinaClient(validAddress, querier, logger)
	require.NoError(t, err)

	got, err := client.FetchActions(context.Background(), 90)
	require.Nil(t, got)
	require.ErrorIs(t, err, apperrors.ErrBestChainBlockNotFound)
	require.Equal(t, []int64{90}, querier.pointLookupHeights)
	require.Empty(t, querier.actionBlockIDs)
}

func TestFetchActionsReturnsEmptySliceWhenBestChainBlockHasNoActions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	querier := &trackingQuerier{
		bestChainBlockIDsByHeight: map[int64]int64{
			90: 900,
		},
		actionRowsByBlockID: map[int64][]sqlcdb.ListActionRowsByBlockIDRow{
			900: {},
		},
	}

	client, err := NewMinaClient(validAddress, querier, logger)
	require.NoError(t, err)

	got, err := client.FetchActions(context.Background(), 90)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, []int64{90}, querier.pointLookupHeights)
	require.Equal(t, []int64{900}, querier.actionBlockIDs)
}

func TestFetchActionsWrapsRetryableActionQueryErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	querier := &trackingQuerier{
		bestChainBlockIDsByHeight: map[int64]int64{
			90: 900,
		},
		actionRowsErr: context.DeadlineExceeded,
	}

	client, err := NewMinaClient(validAddress, querier, logger)
	require.NoError(t, err)

	got, err := client.FetchActions(context.Background(), 90)
	require.Nil(t, got)
	require.ErrorIs(t, err, apperrors.ErrQueryConnectionLost)
}

func TestPrimeBestChainRangeWrapsRetryableQueryErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	querier := &trackingQuerier{
		rangeErr: context.DeadlineExceeded,
	}

	client, err := NewMinaClient(validAddress, querier, logger)
	require.NoError(t, err)

	err = client.PrimeBestChainRange(context.Background(), 90, 91)
	require.ErrorIs(t, err, apperrors.ErrQueryConnectionLost)
}

func TestFetchActionsPreservesQuerierRowOrderWithinBlock(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	querier := &trackingQuerier{
		bestChainBlockIDsByHeight: map[int64]int64{
			90: 900,
		},
		actionRowsByBlockID: map[int64][]sqlcdb.ListActionRowsByBlockIDRow{
			900: {
				actionQueryRow(90, "1", "7"),
				actionQueryRow(90, "2", "42"),
			},
		},
	}

	client, err := NewMinaClient(validAddress, querier, logger)
	require.NoError(t, err)

	got, err := client.FetchActions(context.Background(), 90)
	require.NoError(t, err)
	require.Len(t, got, 2)

	require.Equal(t, int64(7), got[0].Amount)
	require.Equal(t, actions.ActionType_DEPOSIT, got[0].ActionType)
	require.Equal(t, int64(42), got[1].Amount)
	require.Equal(t, actions.ActionType_WITHDRAW, got[1].ActionType)
}

type rangeKey struct {
	startHeight int64
	endHeight   int64
}

type trackingQuerier struct {
	bestChainRowsByRange      map[rangeKey][]sqlcdb.ListBestChainBlockIDsInRangeRow
	bestChainBlockIDsByHeight map[int64]int64
	actionRowsByBlockID       map[int64][]sqlcdb.ListActionRowsByBlockIDRow
	primedRanges              []rangeKey
	pointLookupHeights        []int64
	actionBlockIDs            []int64
	latestHeightErr           error
	rangeErr                  error
	pointLookupErr            error
	actionRowsErr             error
}

func (q *trackingQuerier) GetLatestBlockHeight(context.Context) (int64, error) {
	if q.latestHeightErr != nil {
		return 0, q.latestHeightErr
	}
	return 0, nil
}

func (q *trackingQuerier) GetBestChainBlockIDAtHeight(_ context.Context, height int64) (sqlcdb.GetBestChainBlockIDAtHeightRow, error) {
	q.pointLookupHeights = append(q.pointLookupHeights, height)

	if q.pointLookupErr != nil {
		return sqlcdb.GetBestChainBlockIDAtHeightRow{}, q.pointLookupErr
	}

	blockID, ok := q.bestChainBlockIDsByHeight[height]
	if !ok {
		return sqlcdb.GetBestChainBlockIDAtHeightRow{}, pgx.ErrNoRows
	}

	return sqlcdb.GetBestChainBlockIDAtHeightRow{
		ID:     blockID,
		Height: height,
	}, nil
}

func (q *trackingQuerier) ListBestChainBlockIDsInRange(_ context.Context, arg sqlcdb.ListBestChainBlockIDsInRangeParams) ([]sqlcdb.ListBestChainBlockIDsInRangeRow, error) {
	key := rangeKey{startHeight: arg.StartHeight, endHeight: arg.EndHeight}
	q.primedRanges = append(q.primedRanges, key)
	if q.rangeErr != nil {
		return nil, q.rangeErr
	}
	return q.bestChainRowsByRange[key], nil
}

func (q *trackingQuerier) ListActionRowsByBlockID(_ context.Context, arg sqlcdb.ListActionRowsByBlockIDParams) ([]sqlcdb.ListActionRowsByBlockIDRow, error) {
	q.actionBlockIDs = append(q.actionBlockIDs, arg.BlockID)
	if q.actionRowsErr != nil {
		return nil, q.actionRowsErr
	}
	return q.actionRowsByBlockID[arg.BlockID], nil
}

func validActionQueryRow(height int64) sqlcdb.ListActionRowsByBlockIDRow {
	return actionQueryRow(height, "1", "42")
}

func actionQueryRow(height int64, actionType string, amount string) sqlcdb.ListActionRowsByBlockIDRow {
	return sqlcdb.ListActionRowsByBlockIDRow{
		Height: height,
		Data:   []string{actionType, "7", "1", amount},
	}
}
