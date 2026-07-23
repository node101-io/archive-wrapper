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

type Query struct {
	logger                *slog.Logger
	db                    *database.DbManager
	maxActionRangeHeights int64
}

func NewQuery(db *database.DbManager, logger *slog.Logger, maxActionRangeHeights int64) (*Query, error) {
	if logger == nil {
		return nil, apperrors.ErrNilLogger
	}

	if db == nil {
		return nil, apperrors.ErrNilManager
	}

	if err := db.Validate(); err != nil {
		return nil, err
	}
	if maxActionRangeHeights <= 0 {
		return nil, apperrors.ErrMaxActionRangeRequired
	}

	logger = logger.With("component", "query")
	logger.Info("query service initialized", "max_action_range_heights", maxActionRangeHeights)

	return &Query{
		logger:                logger,
		db:                    db,
		maxActionRangeHeights: maxActionRangeHeights,
	}, nil
}

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

	if in.EndBlockHeight-in.StartBlockHeight >= q.maxActionRangeHeights {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"requested block range exceeds maximum width of %d heights",
			q.maxActionRangeHeights,
		)
	}

	earliestHeight, err := q.db.GetStartBlockHeight()
	if errors.Is(err, leveldb.ErrNotFound) {
		_, latestErr := q.db.GetBlockHeight()
		if errors.Is(latestErr, leveldb.ErrNotFound) {
			return nil, status.Error(
				codes.FailedPrecondition,
				"indexer has not processed any blocks yet",
			)
		}
		if latestErr != nil {
			q.logger.ErrorContext(ctx, "failed to read latest processed block height while checking indexed bounds", "err", latestErr)
			return nil, status.Error(
				codes.Internal,
				"failed to read latest block height",
			)
		}
		return nil, status.Error(
			codes.FailedPrecondition,
			"indexer start block height is not initialized",
		)
	}
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to read earliest indexed block height", "err", err)
		return nil, status.Error(
			codes.Internal,
			"failed to read earliest indexed block height",
		)
	}

	if in.StartBlockHeight < earliestHeight {
		q.logger.WarnContext(
			ctx,
			"query requested range below earliest indexed height",
			"start_block_height",
			in.StartBlockHeight,
			"end_block_height",
			in.EndBlockHeight,
			"earliest_indexed_height",
			earliestHeight,
		)
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"start block height %d is lower than earliest indexed block %d",
			in.StartBlockHeight,
			earliestHeight,
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
