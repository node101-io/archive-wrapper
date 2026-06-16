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

	height, err := q.db.GetBlockHeight()
	if err != nil {
		return nil, err
	}

	if height < in.BlockHeight {
		return nil, cosmosErrors.Wrap(errors.ErrInvalidBlockHeight, "higher than latest block")
	}

	dbRecord, err := q.db.Get(in.BlockHeight)
	if err != nil {
		return nil, err
	}

	return &QueryActionsByBlockHeightResponse{
		Actions: dbRecord.Actions,
	}, nil
}
