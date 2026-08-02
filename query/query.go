package query

import (
	"context"
	"errors"
	"log/slog"

	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/syndtr/goleveldb/leveldb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Query serves gRPC reads against the locally indexed LevelDB data.
type Query struct {
	logger           *slog.Logger
	db               *database.DbManager
	startBlockHeight int64
	maxBlockRange    int64
}

// NewQuery constructs a Query service with the configured range guard.
func NewQuery(
	db *database.DbManager,
	logger *slog.Logger,
	startBlockHeight int64,
	maxBlockRange int64,
) (*Query, error) {
	if logger == nil {
		return nil, apperrors.ErrNilLogger
	}

	if db == nil {
		return nil, apperrors.ErrNilManager
	}

	if err := db.Validate(); err != nil {
		return nil, err
	}
	if startBlockHeight <= 0 {
		return nil, apperrors.ErrStartBlockHeightRequired
	}
	if maxBlockRange <= 0 {
		return nil, apperrors.ErrMaxBlockRangeRequired
	}

	logger = logger.With("component", "query")
	logger.Info(
		"query service initialized",
		"start_block_height",
		startBlockHeight,
		"max_block_range",
		maxBlockRange,
	)

	return &Query{
		logger:           logger,
		db:               db,
		startBlockHeight: startBlockHeight,
		maxBlockRange:    maxBlockRange,
	}, nil
}

// GetActionsInRange returns all indexed actions for the inclusive block range.
// It rejects ranges outside the indexed bounds and ranges wider than maxBlockRange.
func (q *Query) GetActionsInRange(
	ctx context.Context,
	in *QueryGetActionsInRangeRequest,
) (*QueryGetActionsInRangeResponse, error) {
	if q == nil {
		return nil, status.Error(
			codes.FailedPrecondition,
			"query service is not initialized",
		)
	}

	if q.db == nil {
		return nil, status.Error(
			codes.FailedPrecondition,
			"database is not initialized",
		)
	}

	if q.logger == nil {
		return nil, status.Error(
			codes.FailedPrecondition,
			"logger is not initialized",
		)
	}

	if in == nil {
		return nil, status.Error(
			codes.InvalidArgument,
			"request is required",
		)
	}

	if in.StartBlockHeight <= 0 {
		return nil, status.Error(
			codes.InvalidArgument,
			"start block height must be greater than 0",
		)
	}

	if in.EndBlockHeight <= 0 {
		return nil, status.Error(
			codes.InvalidArgument,
			"end block height must be greater than 0",
		)
	}

	if in.StartBlockHeight > in.EndBlockHeight {
		return nil, status.Error(
			codes.InvalidArgument,
			"start block height must be less than or equal to end block height",
		)
	}

	if in.EndBlockHeight-in.StartBlockHeight >= q.maxBlockRange {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"requested block range exceeds maximum width of %d heights",
			q.maxBlockRange,
		)
	}

	// Start height comes from deployment metadata validated before startup.
	if in.StartBlockHeight < q.startBlockHeight {
		q.logger.WarnContext(
			ctx,
			"query requested range below earliest indexed height",
			"start_block_height",
			in.StartBlockHeight,
			"end_block_height",
			in.EndBlockHeight,
			"earliest_indexed_height",
			q.startBlockHeight,
		)
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"start block height %d is lower than earliest indexed block %d",
			in.StartBlockHeight,
			q.startBlockHeight,
		)
	}

	// The latest processed cursor tells us whether this range is queryable yet.
	latestHeight, err := q.db.GetBlockHeight()
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, status.Error(
			codes.FailedPrecondition,
			"indexer has not processed any blocks yet",
		)
	}
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to read latest processed block height", "err", err)
		return nil, status.Error(
			codes.Internal,
			"failed to read latest block height",
		)
	}

	if in.EndBlockHeight > latestHeight {
		q.logger.WarnContext(
			ctx,
			"query requested range above latest processed height",
			"start_block_height",
			in.StartBlockHeight,
			"end_block_height",
			in.EndBlockHeight,
			"latest_processed_height",
			latestHeight,
		)
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"end block height %d is higher than latest processed block %d",
			in.EndBlockHeight,
			latestHeight,
		)
	}

	result := make([]*actions.Action, 0)

	for height := in.StartBlockHeight; height <= in.EndBlockHeight; height++ {
		if err := ctx.Err(); err != nil {
			return nil, status.FromContextError(err).Err()
		}

		record, err := q.db.Get(height)
		if errors.Is(err, leveldb.ErrNotFound) {
			// Inside the processed range, a missing record is treated as an empty block.
			continue
		}
		if err != nil {
			q.logger.ErrorContext(ctx, "failed to read block actions", "block_height", height, "err", err)
			return nil, status.Error(
				codes.Internal,
				"failed to read block actions",
			)
		}

		result = append(result, record.Actions...)
	}

	q.logger.InfoContext(
		ctx,
		"query returned actions in range",
		"start_block_height",
		in.StartBlockHeight,
		"end_block_height",
		in.EndBlockHeight,
		"actions",
		len(result),
	)

	return &QueryGetActionsInRangeResponse{
		Actions: result,
	}, nil
}
