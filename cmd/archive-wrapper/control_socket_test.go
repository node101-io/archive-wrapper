package main

import (
	"fmt"
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
	socketPath := filepath.Join("/tmp", fmt.Sprintf("archive-wrapper-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})

	listener, err := net.Listen(network, socketPath)
	require.NoError(t, err)

	replacement := &replacementListener{Listener: listener, path: socketPath}
	server := startControlServer(replacement, socketPath, func() {}, logger)
	t.Cleanup(func() {
		if replacement.replacement != nil {
			_ = replacement.replacement.Close()
		}
	})

	require.NoError(t, server.Close())
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("control server did not stop")
	}

	info, err := os.Stat(socketPath)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSocket)

	conn, err := net.DialTimeout(network, socketPath, time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
}

func TestControlServerCloseIsIdempotentAndRemovesSocket(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	socketPath := filepath.Join(t.TempDir(), "control.sock")
	server, err := listenControlSocket(socketPath, func() {}, logger)
	require.NoError(t, err)

	require.NoError(t, server.Close())
	require.NoError(t, server.Close())
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("control server did not stop")
	}
	_, err = os.Stat(socketPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestControlServerStopCommandCancelsRuntime(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	socketPath := filepath.Join(t.TempDir(), "control.sock")
	canceled := make(chan struct{})
	server, err := listenControlSocket(socketPath, func() { close(canceled) }, logger)
	require.NoError(t, err)
	defer func() { require.NoError(t, server.Close()) }()

	require.NoError(t, runStop(socketPath, logger))
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("STOP did not cancel the runtime")
	}
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("control server did not finish after STOP")
	}
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
