package query

import (
	"context"
	"testing"

	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/database"

	"github.com/stretchr/testify/require"
)

const blockHeightDatabaseKey = "db-key"

func TestQuery(t *testing.T) {

	manager, err := database.NewDbManager(t.TempDir(), blockHeightDatabaseKey)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	want := actions.DbRecord{
		Key: 7,
		Actions: []*actions.Action{
			{
				BlockHeight: 7,
				FeePayer:    []byte("alice"),
				ActionType:  actions.ActionType_DEPOSIT,
				Amount:      42,
			},
		},
	}

	err = manager.Insert(want)
	require.NoError(t, err)

	err = manager.InsertBlockHeight(want.Key)
	require.NoError(t, err)

	q := NewQuery(manager)
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
