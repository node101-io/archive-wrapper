package db

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListActionRowsByBlockIDUsesCanonicalOrdering(t *testing.T) {
	normalizedQuery := strings.Join(strings.Fields(listActionRowsByBlockID), " ")

	require.Contains(
		t,
		normalizedQuery,
		"ORDER BY height, sequence_no, account_update_index, action_index, zkapp_command_id, account_update_id",
	)
	require.NotContains(t, normalizedQuery, "ORDER BY height, fee_payer")
}
