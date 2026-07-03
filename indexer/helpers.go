package indexer

import (
	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
)

// Creates a DbRecord for the block height with its corresponding actions.
func IndexActions(items []actions.Action, height int64) (actions.DbRecord, error) {

	if len(items) == 0 {
		return actions.DbRecord{}, nil
	}

	var actionList []*actions.Action

	for _, item := range items {
		if item.BlockHeight != height {
			return actions.DbRecord{}, apperrors.ErrInvalidBlockHeight
		}
		actionList = append(actionList, &item)
	}

	return actions.DbRecord{
		Key:     height,
		Actions: actionList,
	}, nil
}
