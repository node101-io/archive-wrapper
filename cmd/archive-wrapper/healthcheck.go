package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
)

const defaultHealthcheckTimeout = 3 * time.Second

func runHealthcheckCommand(
	args []string,
	lookupEnv func(string) (string, bool),
) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "path to configuration file")
	address := fs.String("address", "", "gRPC health target")
	timeout := fs.Duration("timeout", defaultHealthcheckTimeout, "healthcheck timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("healthcheck does not accept positional arguments: %q", fs.Args())
	}
	if *timeout <= 0 {
		return errors.New("healthcheck timeout must be greater than zero")
	}

	resolvedAddress, err := resolveHealthcheckAddress(
		*address,
		flagWasSet(fs, "address"),
		*configPath,
		flagWasSet(fs, "config"),
		lookupEnv,
	)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	return checkQueryHealth(ctx, resolvedAddress)
}

func resolveHealthcheckAddress(
	flagAddress string,
	addressFlagSet bool,
	flagConfigPath string,
	configFlagSet bool,
	lookupEnv func(string) (string, bool),
) (string, error) {
	if lookupEnv == nil {
		return "", errors.New("environment lookup is required")
	}

	if addressFlagSet {
		return healthDialAddress(flagAddress)
	}
	if address, ok := lookupEnv("ARCHIVE_WRAPPER_HEALTHCHECK_ADDRESS"); ok {
		return healthDialAddress(address)
	}

	configPath := strings.TrimSpace(flagConfigPath)
	if configFlagSet {
		if configPath == "" {
			return "", apperrors.ErrConfigPathRequired
		}
	} else {
		value, ok := lookupEnv("ARCHIVE_WRAPPER_CONFIG")
		if !ok || strings.TrimSpace(value) == "" {
			return "", apperrors.ErrConfigPathRequired
		}
		configPath = strings.TrimSpace(value)
	}

	address, err := config.ResolveGRPCListenAddress(configPath)
	if err != nil {
		return "", err
	}
	return healthDialAddress(address)
}

func healthDialAddress(address string) (string, error) {
	if address != strings.TrimSpace(address) || address == "" {
		return "", apperrors.ErrGRPCAddressRequired
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("%w: %v", apperrors.ErrInvalidGRPCListenAddress, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return "", fmt.Errorf("%w: health target host must be an unscoped literal IP", apperrors.ErrInvalidGRPCListenAddress)
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return "", fmt.Errorf("%w: port must be between 1 and 65535", apperrors.ErrInvalidGRPCListenAddress)
	}

	switch {
	case ip.Is4() && ip.IsUnspecified():
		ip = netip.MustParseAddr("127.0.0.1")
	case ip.Is6() && ip.IsUnspecified():
		ip = netip.IPv6Loopback()
	}
	return net.JoinHostPort(ip.String(), port), nil
}

func checkQueryHealth(ctx context.Context, address string) error {
	connection, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return errors.New("query health connection failed")
	}
	defer connection.Close()

	response, err := grpcHealthV1.NewHealthClient(connection).Check(
		ctx,
		&grpcHealthV1.HealthCheckRequest{Service: queryGRPCServiceName},
	)
	if err != nil {
		return errors.New("query health check failed")
	}
	if response.Status != grpcHealthV1.HealthCheckResponse_SERVING {
		return fmt.Errorf("query service is not serving: %s", response.Status)
	}
	return nil
}

func runShortCommand(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	lookupEnv func(string) (string, bool),
) (bool, int) {
	if len(args) == 0 {
		return false, exitCodeSuccess
	}

	switch strings.ToLower(args[0]) {
	case "healthcheck":
		if err := runHealthcheckCommand(args[1:], lookupEnv); err != nil {
			_, _ = fmt.Fprintf(stderr, "healthcheck failed: %v\n", err)
			return true, exitCodeFailure
		}
		return true, exitCodeSuccess
	case "version":
		if len(args) != 1 {
			_, _ = fmt.Fprintln(stderr, "version does not accept arguments")
			return true, exitCodeFailure
		}
		if stdout == nil {
			_, _ = fmt.Fprintln(stderr, "stdout writer is required")
			return true, exitCodeFailure
		}
		if err := writeVersion(stdout); err != nil {
			return true, exitCodeFailure
		}
		return true, exitCodeSuccess
	default:
		return false, exitCodeSuccess
	}
}
