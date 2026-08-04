package main

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestQueryReadinessGateBlocksOnlyQueryService(t *testing.T) {
	gate := &queryReadinessGate{}
	handlerCalls := 0
	handler := func(context.Context, any) (any, error) {
		handlerCalls++
		return "ok", nil
	}

	_, err := gate.unaryServerInterceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/query.Query/GetMinaBlockHeight"},
		handler,
	)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Zero(t, handlerCalls)

	for _, method := range []string{
		"/grpc.health.v1.Health/Check",
		"/diagnostics.DiagnosticsService/GetStatus",
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	} {
		response, err := gate.unaryServerInterceptor(
			context.Background(),
			nil,
			&grpc.UnaryServerInfo{FullMethod: method},
			handler,
		)
		require.NoError(t, err)
		require.Equal(t, "ok", response)
	}
	require.Equal(t, 3, handlerCalls)

	gate.setReady(true)
	response, err := gate.unaryServerInterceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/query.Query/GetActionsInRange"},
		handler,
	)
	require.NoError(t, err)
	require.Equal(t, "ok", response)
	require.Equal(t, 4, handlerCalls)
}

func TestQueryReadinessGateSupportsConcurrentTransitions(t *testing.T) {
	gate := &queryReadinessGate{}
	var group sync.WaitGroup

	for i := 0; i < 100; i++ {
		group.Add(2)
		go func(ready bool) {
			defer group.Done()
			gate.setReady(ready)
		}(i%2 == 0)
		go func() {
			defer group.Done()
			_, _ = gate.unaryServerInterceptor(
				context.Background(),
				nil,
				&grpc.UnaryServerInfo{FullMethod: "/query.Query/GetMinaBlockHeight"},
				func(context.Context, any) (any, error) { return nil, nil },
			)
		}()
	}

	group.Wait()
}
