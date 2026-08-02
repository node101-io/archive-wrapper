package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/node101-io/archive-wrapper/config"
)

// The control channel is local-only and never exposed over TCP.
const network = "unix"

// The protocol is deliberately small so start, stop, and liveness checks agree.
const (
	controlSocketReadTimeout  = time.Second
	controlSocketPingCommand  = "PING\n"
	controlSocketPongResponse = "PONG\n"
	controlSocketStopCommand  = "STOP\n"
)

// closeControlSocketListener leaves filesystem cleanup to the listener implementation.
func closeControlSocketListener(ln net.Listener) error {
	if err := ln.Close(); err != nil {
		return fmt.Errorf("close control socket listener: %w", err)
	}
	return nil
}

// runStop sends a single shutdown command to the running wrapper process.
func runStop(sockPath string, logger *slog.Logger) (retErr error) {
	cliLogger := logger.With("component", "cli")

	cliLogger.Info("sending stop request", "socket_path", sockPath)

	c, err := net.DialTimeout(network, sockPath, time.Second)
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close stop connection: %w", err))
		}
	}()

	if _, err := io.WriteString(c, controlSocketStopCommand); err != nil {
		return fmt.Errorf("write stop command: %w", err)
	}

	cliLogger.Info("stop request sent", "socket_path", sockPath)

	return
}

// resolveStopSocketPath applies the explicit flag, config, then environment precedence.
func resolveStopSocketPath(socketPath, configPath, envSocketPath string) (string, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath != "" {
		return socketPath, nil
	}

	configPath = strings.TrimSpace(configPath)
	if configPath != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return "", fmt.Errorf("load stop config: %w", err)
		}
		return cfg.ControlSocketPath, nil
	}

	envSocketPath = strings.TrimSpace(envSocketPath)
	if envSocketPath != "" {
		return envSocketPath, nil
	}

	return "", fmt.Errorf(
		"--config or --socket-path is required unless ARCHIVE_WRAPPER_CONFIG or ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH is set",
	)
}

// listenControlSocket accepts local commands until the listener closes or STOP arrives.
func listenControlSocket(sockPath string, cancel context.CancelFunc, logger *slog.Logger) (net.Listener, error) {
	controlLogger := logger.With("component", "control_socket")

	if err := prepareControlSocket(sockPath, logger); err != nil {
		return nil, err
	}

	ln, err := net.Listen(network, sockPath)
	if err != nil {
		return nil, err
	}

	controlLogger.Info("control socket listening", "socket_path", sockPath)

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					controlLogger.Error("control socket accept failed", "socket_path", sockPath, "err", err)
				}
				return
			}

			shouldStop := handleControlSocketConn(c, sockPath, controlLogger)
			if shouldStop {
				controlLogger.Info("stop request received via control socket", "socket_path", sockPath)
				cancel()
				return
			}
		}
	}()

	return ln, nil
}

// prepareControlSocket rejects live owners and removes only demonstrably stale sockets.
func prepareControlSocket(sockPath string, logger *slog.Logger) error {
	controlLogger := logger.With("component", "control_socket")

	info, err := os.Lstat(sockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a unix socket", sockPath)
	}

	c, err := net.DialTimeout(network, sockPath, 300*time.Millisecond)
	if err == nil {
		if err := probeLiveControlSocket(c); err != nil {
			return fmt.Errorf("probe control socket %s: %w", sockPath, err)
		}
		controlLogger.Warn("control socket already in use", "socket_path", sockPath)
		return fmt.Errorf("control socket already in use: %s", sockPath)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) || errors.Is(opErr.Err, syscall.ENOENT) {
			// Refused or missing means the old socket file is stale and safe to remove.
			controlLogger.Warn("removing stale control socket", "socket_path", sockPath)
			if err := os.Remove(sockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("probe control socket %s: %w", sockPath, err)
}

// handleControlSocketConn processes one bounded PING or STOP request.
func handleControlSocketConn(c net.Conn, sockPath string, logger *slog.Logger) bool {
	defer func() {
		if err := c.Close(); err != nil {
			logger.Warn("close control socket connection failed", "socket_path", sockPath, "err", err)
		}
	}()

	if err := c.SetDeadline(time.Now().Add(controlSocketReadTimeout)); err != nil {
		logger.Warn("set control socket deadline failed", "socket_path", sockPath, "err", err)
		return false
	}

	command, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			logger.Warn("control socket connection closed without command", "socket_path", sockPath)
			return false
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			logger.Warn("control socket command timed out", "socket_path", sockPath)
			return false
		}
		logger.Warn("read control socket command failed", "socket_path", sockPath, "err", err)
		return false
	}

	switch strings.TrimSpace(command) {
	case "PING":
		if _, err := io.WriteString(c, controlSocketPongResponse); err != nil {
			logger.Warn("write control socket pong failed", "socket_path", sockPath, "err", err)
		}
		return false
	case "STOP":
		return true
	default:
		logger.Warn("unknown control socket command", "socket_path", sockPath, "command", strings.TrimSpace(command))
		return false
	}
}

// probeLiveControlSocket confirms that an existing socket belongs to a live wrapper.
func probeLiveControlSocket(c net.Conn) (retErr error) {
	defer func() {
		if err := c.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close control socket: %w", err))
		}
	}()

	if err := c.SetDeadline(time.Now().Add(controlSocketReadTimeout)); err != nil {
		return fmt.Errorf("set control socket probe deadline: %w", err)
	}

	if _, err := io.WriteString(c, controlSocketPingCommand); err != nil {
		return fmt.Errorf("write control socket ping: %w", err)
	}

	response, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read control socket pong: %w", err)
	}
	if response != controlSocketPongResponse {
		return fmt.Errorf("unexpected control socket response %q", strings.TrimSpace(response))
	}

	return nil
}
