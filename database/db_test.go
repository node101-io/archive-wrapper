package database

import (
	"testing"

	archiveTypes "archive-wrapper/types"

	"github.com/stretchr/testify/require"
)

func TestDbManager_InsertThenGet(t *testing.T) {

	manager, err := NewDbManager()

	require.NoError(t, err)
	require.NotNil(t, manager)

	want := archiveTypes.DbRecord{
		Key: 7,
		Actions: []*archiveTypes.Action{
			{
				BlockHeight: 7,
				FeePayer:    []byte("alice"),
				ActionType:  archiveTypes.ActionType_DEPOSIT,
				Amount:      42,
			},
		},
	}

	err = manager.Insert(want)
	require.NoError(t, err)

	got, err := manager.Get(want.Key)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, got.Key, want.Key)
	require.Equal(t, len(got.Actions), 1) // Because we inserted only 1 action

	// Ensure they have the same values
	require.Equal(t, got.Actions[0].BlockHeight, want.Actions[0].BlockHeight)
	require.Equal(t, got.Actions[0].FeePayer, want.Actions[0].FeePayer)
	require.Equal(t, got.Actions[0].ActionType, want.Actions[0].ActionType)
	require.Equal(t, got.Actions[0].Amount, want.Actions[0].Amount)
}
