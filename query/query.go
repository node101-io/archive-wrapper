package query

import (
	"context"
	"errors"

	"github.com/node101-io/archive-wrapper/database"
	"github.com/syndtr/goleveldb/leveldb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Query struct {
	db *database.DbManager
}

func NewQuery(db *database.DbManager) *Query {
	return &Query{
		db: db,
	}
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
		return nil, status.Error(
			codes.FailedPrecondition,
			"indexer has not processed any blocks yet",
		)
	}
	if err != nil {
		return nil, status.Error(
			codes.Internal,
			"failed to read latest block height",
		)
	}

	if in.BlockHeight > latestHeight {
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"block height %d is higher than latest processed block %d",
			in.BlockHeight,
			latestHeight,
		)
	}

	exists, err := q.db.Has(in.BlockHeight)
	if err != nil {
		return nil, status.Error(
			codes.Internal,
			"failed to check block actions",
		)
	}
	if !exists {
		return nil, status.Errorf(
			codes.NotFound,
			"no actions found for block %d",
			in.BlockHeight,
		)
	}

	record, err := q.db.Get(in.BlockHeight)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, status.Errorf(
			codes.NotFound,
			"no actions found for block %d",
			in.BlockHeight,
		)
	}
	if err != nil {
		return nil, status.Error(
			codes.Internal,
			"failed to read block actions",
		)
	}

	return &QueryActionsByBlockHeightResponse{
		Actions: record.Actions,
	}, nil
}
