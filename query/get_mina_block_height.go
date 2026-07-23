package query

import (
	"context"
	"errors"

	"github.com/syndtr/goleveldb/leveldb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q *Query) GetMinaBlockHeight(
	ctx context.Context,
	in *QueryGetMinaBlockHeightRequest,
) (*QueryGetMinaBlockHeightResponse, error) {
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

	lastIndexed, err := q.db.GetBlockHeight()
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

	return &QueryGetMinaBlockHeightResponse{
		BlockHeight: lastIndexed,
	}, nil
}
