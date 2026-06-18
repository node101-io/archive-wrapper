package query

import (
	"archive-wrapper/database"
	"archive-wrapper/errors"
	"context"

	cosmosErrors "cosmossdk.io/errors"
)

type Query struct {
	db *database.DbManager
}

func (q *Query) ActionsByBlockHeight(ctx context.Context, in *QueryActionsByBlockHeightRequest) (*QueryActionsByBlockHeightResponse, error) {

	if q == nil {
		return nil, errors.ErrNilQuery
	}

	if q.db == nil {
		return nil, errors.ErrNilManager
	}

	if in == nil {
		return nil, errors.ErrInvalidRequest
	}

	height, err := q.db.GetBlockHeight()
	if err != nil {
		return nil, err
	}

	if in.BlockHeight < 0 {
		return nil, cosmosErrors.Wrap(errors.ErrInvalidBlockHeight, "block height must be greater than 0")
	}

	if height < in.BlockHeight {
		return nil, cosmosErrors.Wrap(errors.ErrInvalidBlockHeight, "higher than latest block")
	}

	exists, err := q.db.Has(in.BlockHeight)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.ErrNoActionsInBlock
	}

	dbRecord, err := q.db.Get(in.BlockHeight)
	if err != nil {
		return nil, err
	}

	return &QueryActionsByBlockHeightResponse{
		Actions: dbRecord.Actions,
	}, nil
}
