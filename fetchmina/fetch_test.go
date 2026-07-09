package fetchmina

import (
	"errors"
	"log/slog"
	"testing"

	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/stretchr/testify/require"
)

const validAddress = "B62qjTpSX2R4fyqJrC9pzvm5PSZb5GMaYBXh8di3TBn6pPC9XXYFC9k"

func TestNewMinaClientRejectsNilQueries(t *testing.T) {

	logger := slog.Default()
	require.NotNil(t, logger)

	client, err := NewMinaClient(validAddress, nil, logger)

	require.Nil(t, client)
	require.Error(t, err)
	require.True(t, errors.Is(err, apperrors.ErrNilQueries))
}

func TestNewMinaClientRejectsInvalidContractAddress(t *testing.T) {

	logger := slog.Default()
	require.NotNil(t, logger)

	client, err := NewMinaClient("not-an-address", &sqlcdb.Queries{}, logger)
	require.Nil(t, client)
	require.Error(t, err)
}

func TestActionFromRawDataBuildsAction(t *testing.T) {
	action, err := actionFromRawData(99, validAddress, []string{"1", "ignored", "ignored", "42"})
	require.NoError(t, err)
	require.NotNil(t, action)

	require.Equal(t, int64(99), action.BlockHeight)
	require.Equal(t, actions.ActionType_DEPOSIT, action.ActionType)
	require.Equal(t, int64(42), action.Amount)
	require.NotEmpty(t, action.FeePayer)
}
