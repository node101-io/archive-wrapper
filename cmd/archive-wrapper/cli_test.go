package main

import (
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseControlSocketListenerPreservesReplacementSocket(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	socketPath := filepath.Join(t.TempDir(), "control.sock")

	listener, err := listenControlSocket(socketPath, func() {}, logger)
	require.NoError(t, err)

	replacement := &replacementListener{Listener: listener, path: socketPath}
	t.Cleanup(func() {
		if replacement.replacement != nil {
			_ = replacement.replacement.Close()
		}
	})

	require.NoError(t, closeControlSocketListener(replacement))

	info, err := os.Stat(socketPath)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSocket)

	conn, err := net.DialTimeout(network, socketPath, time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
}

type replacementListener struct {
	net.Listener
	path        string
	replacement net.Listener
}

func (l *replacementListener) Close() error {
	if err := l.Listener.Close(); err != nil {
		return err
	}

	replacement, err := net.Listen(network, l.path)
	if err != nil {
		return err
	}
	l.replacement = replacement
	return nil
}
