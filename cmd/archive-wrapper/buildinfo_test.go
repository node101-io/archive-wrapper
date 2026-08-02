package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteVersion(t *testing.T) {
	originalVersion, originalCommit, originalDate := version, commitSHA, buildDate
	t.Cleanup(func() {
		version, commitSHA, buildDate = originalVersion, originalCommit, originalDate
	})
	version, commitSHA, buildDate = "v1.2.3", "abc123", "2026-08-03T00:00:00Z"

	var output bytes.Buffer
	require.NoError(t, writeVersion(&output))
	require.Equal(
		t,
		"archive-wrapper version=v1.2.3 commit=abc123 build_date=2026-08-03T00:00:00Z\n",
		output.String(),
	)
}
