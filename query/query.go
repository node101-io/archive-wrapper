package query

import (
	"context"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"

	cosmoserrors "cosmossdk.io/errors"
)

type Query struct {
	db *database.DbManager
}

func NewQuery(db *database.DbManager) *Query {
	return &Query{
		db: db,
	}
}

func (q *Query) ActionsByBlockHeight(ctx context.Context, in *QueryActionsByBlockHeightRequest) (*QueryActionsByBlockHeightResponse, error) {

	if q == nil {
		return nil, apperrors.ErrNilQuery
	}

	if q.db == nil {
		return nil, apperrors.ErrNilManager
	}

	if in == nil {
		return nil, apperrors.ErrInvalidRequest
	}

	height, err := q.db.GetBlockHeight()
	if err != nil {
		return nil, err
	}

	if in.BlockHeight < 0 {
		return nil, cosmoserrors.Wrap(apperrors.ErrInvalidBlockHeight, "block height must be greater than 0")
	}

	if height < in.BlockHeight {
		return nil, cosmoserrors.Wrap(apperrors.ErrInvalidBlockHeight, "higher than latest block")
	}

	exists, err := q.db.Has(in.BlockHeight)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, apperrors.ErrNoActionsInBlock
	}

	dbRecord, err := q.db.Get(in.BlockHeight)
	if err != nil {
		return nil, err
	}

	return &QueryActionsByBlockHeightResponse{
		Actions: dbRecord.Actions,
	}, nil
}
