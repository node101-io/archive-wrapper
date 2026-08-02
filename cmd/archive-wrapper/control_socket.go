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
	"sync"
	"syscall"
	"time"

	"github.com/node101-io/archive-wrapper/apperrors"
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

type controlServer struct {
	listener  net.Listener
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// Close stops the accept loop. The Unix listener owns socket-file cleanup.
func (server *controlServer) Close() error {
	if server == nil {
		return nil
	}
	server.closeOnce.Do(func() {
		if err := server.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			server.closeErr = fmt.Errorf("close control socket listener: %w", err)
		}
	})
	return server.closeErr
}

// Done is closed after the accept loop exits.
func (server *controlServer) Done() <-chan struct{} {
	if server == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return server.done
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

// resolveStopSocketPath applies CLI > environment > config precedence.
func resolveStopSocketPath(socketPath string, socketPathSet bool, configPath string, configPathSet bool) (string, error) {
	if socketPathSet {
		if strings.TrimSpace(socketPath) == "" {
			return "", apperrors.ErrControlSocketPathRequired
		}
		return strings.TrimSpace(socketPath), nil
	}

	if envSocketPath, ok := os.LookupEnv("ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH"); ok {
		if strings.TrimSpace(envSocketPath) == "" {
			return "", fmt.Errorf("%w: ARCHIVE_WRAPPER_CONTROL_SOCKET_PATH is empty", apperrors.ErrControlSocketPathRequired)
		}
		return strings.TrimSpace(envSocketPath), nil
	}

	if configPathSet || strings.TrimSpace(configPath) != "" {
		if strings.TrimSpace(configPath) == "" {
			return "", apperrors.ErrConfigPathRequired
		}
		cfg, err := config.Load(strings.TrimSpace(configPath))
		if err != nil {
			return "", fmt.Errorf("load stop config: %w", err)
		}
		return cfg.ControlSocketPath, nil
	}

	if envConfigPath, ok := os.LookupEnv("ARCHIVE_WRAPPER_CONFIG"); ok {
		if strings.TrimSpace(envConfigPath) == "" {
			return "", fmt.Errorf("%w: ARCHIVE_WRAPPER_CONFIG is empty", apperrors.ErrConfigPathRequired)
		}
		cfg, err := config.Load(strings.TrimSpace(envConfigPath))
		if err != nil {
			return "", fmt.Errorf("load stop config: %w", err)
		}
		return cfg.ControlSocketPath, nil
	}

	return "", apperrors.ErrControlSocketPathRequired
}

// listenControlSocket accepts local commands until the listener closes or STOP arrives.
func listenControlSocket(sockPath string, cancel context.CancelFunc, logger *slog.Logger) (*controlServer, error) {
	controlLogger := logger.With("component", "control_socket")

	if err := prepareControlSocket(sockPath, logger); err != nil {
		return nil, err
	}

	ln, err := net.Listen(network, sockPath)
	if err != nil {
		return nil, err
	}

	controlLogger.Info("control socket listening", "socket_path", sockPath)

	server := &controlServer{
		listener: ln,
		done:     make(chan struct{}),
	}
	go func() {
		defer close(server.done)
		for {
			c, err := server.listener.Accept()
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

	return server, nil
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
