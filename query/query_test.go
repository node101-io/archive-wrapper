package query

import (
	"archive-wrapper/database"
	"archive-wrapper/types"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuery(t *testing.T) {

	manager, err := database.NewDbManager()
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer manager.Close()

	want := types.DbRecord{
		Key: 7,
		Actions: []*types.Action{
			{
				BlockHeight: 7,
				FeePayer:    []byte("alice"),
				ActionType:  types.ActionType_DEPOSIT,
				Amount:      42,
			},
		},
	}

	err = manager.Insert(want)
	require.NoError(t, err)

	err = manager.InsertBlockHeight(want.Key)
	require.NoError(t, err)

	q := &Query{db: manager}
	got, err := q.ActionsByBlockHeight(context.Background(), &QueryActionsByBlockHeightRequest{
		BlockHeight: 7,
	})

	require.NoError(t, err)
	require.NotNil(t, got)

	// Ensure they have the same values
	require.Equal(t, got.Actions[0].BlockHeight, want.Actions[0].BlockHeight)
	require.Equal(t, got.Actions[0].FeePayer, want.Actions[0].FeePayer)
	require.Equal(t, got.Actions[0].ActionType, want.Actions[0].ActionType)
	require.Equal(t, got.Actions[0].Amount, want.Actions[0].Amount)

}
