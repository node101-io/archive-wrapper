package main

import (
	"context"
	"strings"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// queryReadinessGate prevents business queries while the runtime is unavailable.
type queryReadinessGate struct {
	ready atomic.Bool
}

func (g *queryReadinessGate) setReady(ready bool) {
	if g == nil {
		return
	}
	g.ready.Store(ready)
}

func (g *queryReadinessGate) isReady() bool {
	return g != nil && g.ready.Load()
}

func (g *queryReadinessGate) unaryServerInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	if strings.HasPrefix(info.FullMethod, "/"+queryGRPCServiceName+"/") && !g.isReady() {
		return nil, status.Error(codes.Unavailable, "query service is not ready")
	}
	return handler(ctx, req)
}
