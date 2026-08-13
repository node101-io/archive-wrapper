package actions

import (
	"testing"

	proto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

func TestActionProtoRoundTrip(t *testing.T) {
	want := &Action{
		BlockHeight: 42,
		XCoordinate: append(make([]byte, 31), 1),
		IsOdd:       true,
		ActionType:  ActionType_DEPOSIT,
		Amount:      99,
	}

	encoded, err := proto.Marshal(want)
	require.NoError(t, err)

	var got Action
	require.NoError(t, proto.Unmarshal(encoded, &got))
	require.True(t, proto.Equal(want, &got))
}

func TestDbRecordProtoRoundTrip(t *testing.T) {
	want := &DbRecord{
		Key: 7,
		Actions: []*Action{
			{
				BlockHeight: 7,
				XCoordinate: append(make([]byte, 31), 1),
				IsOdd:       true,
				ActionType:  ActionType_WITHDRAW,
				Amount:      11,
			},
		},
	}

	encoded, err := proto.Marshal(want)
	require.NoError(t, err)

	var got DbRecord
	require.NoError(t, proto.Unmarshal(encoded, &got))
	require.True(t, proto.Equal(want, &got))
}
