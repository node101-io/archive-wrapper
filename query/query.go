package query

import (
	"context"
	"errors"
	"log/slog"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	"github.com/syndtr/goleveldb/leveldb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Query struct {
	logger *slog.Logger
	db     *database.DbManager
}

func NewQuery(db *database.DbManager, logger *slog.Logger) (*Query, error) {
	if logger == nil {
		return nil, apperrors.ErrNilLogger
	}

	if db == nil {
		return nil, apperrors.ErrNilManager
	}

	if err := db.Validate(); err != nil {
		return nil, err
	}

	logger = logger.With("component", "query")
	logger.Info("query service initialized")

	return &Query{
		logger: logger,
		db:     db,
	}, nil
}

func (q *Query) ActionsByBlockHeight(
	ctx context.Context,
	in *QueryActionsByBlockHeightRequest,
) (*QueryActionsByBlockHeightResponse, error) {
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

	if in.BlockHeight <= 0 {
		return nil, status.Error(
			codes.InvalidArgument,
			"block height must be greater than 0",
		)
	}

	latestHeight, err := q.db.GetBlockHeight()
	if errors.Is(err, leveldb.ErrNotFound) {
		q.logger.WarnContext(ctx, "query requested before any block was processed")
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

	if in.BlockHeight > latestHeight {
		q.logger.WarnContext(
			ctx,
			"query requested block above latest processed height",
			"block_height",
			in.BlockHeight,
			"latest_processed_height",
			latestHeight,
		)
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"block height %d is higher than latest processed block %d",
			in.BlockHeight,
			latestHeight,
		)
	}

	exists, err := q.db.Has(in.BlockHeight)
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to check block actions", "block_height", in.BlockHeight, "err", err)
		return nil, status.Error(
			codes.Internal,
			"failed to check block actions",
		)
	}
	if !exists {
		q.logger.InfoContext(
			ctx,
			"query returned empty block",
			"block_height",
			in.BlockHeight,
			"latest_processed_height",
			latestHeight,
		)
		return &QueryActionsByBlockHeightResponse{}, nil
	}

	record, err := q.db.Get(in.BlockHeight)
	if errors.Is(err, leveldb.ErrNotFound) {
		q.logger.InfoContext(
			ctx,
			"query returned empty block",
			"block_height",
			in.BlockHeight,
			"latest_processed_height",
			latestHeight,
		)
		return &QueryActionsByBlockHeightResponse{}, nil
	}
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to read block actions", "block_height", in.BlockHeight, "err", err)
		return nil, status.Error(
			codes.Internal,
			"failed to read block actions",
		)
	}

	q.logger.InfoContext(
		ctx,
		"query returned actions",
		"block_height",
		in.BlockHeight,
		"actions",
		len(record.Actions),
	)

	return &QueryActionsByBlockHeightResponse{
		Actions: record.Actions,
	}, nil
}
