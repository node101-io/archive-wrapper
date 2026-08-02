//go:build container

package container_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
)

const queryServiceName = "query.Query"

type wrapperResult struct {
	height   int64
	amount   int64
	feePayer string
}

func TestConcurrentClients(t *testing.T) {
	addresses := testAddresses(t)
	address := addresses[0]

	results := make(chan wrapperResult, 3)
	workerErrors := make(chan error, 3)
	start := make(chan struct{})
	var workers sync.WaitGroup

	for clientIndex := 0; clientIndex < 3; clientIndex++ {
		clientIndex := clientIndex
		workers.Add(1)
		go func() {
			defer workers.Done()
			connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				workerErrors <- err
				return
			}
			defer connection.Close()
			<-start

			if clientIndex == 0 {
				canceledContext, cancel := context.WithCancel(context.Background())
				cancel()
				_, _ = grpcHealthV1.NewHealthClient(connection).Check(
					canceledContext,
					&grpcHealthV1.HealthCheckRequest{Service: queryServiceName},
				)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := queryWrapper(ctx, connection)
			if err != nil {
				workerErrors <- err
				return
			}
			results <- result
		}()
	}

	close(start)
	workers.Wait()
	close(results)
	close(workerErrors)
	for err := range workerErrors {
		require.NoError(t, err)
	}
	resultCount := 0
	for got := range results {
		require.Equal(t, int64(12), got.height)
		require.Equal(t, int64(42), got.amount)
		require.NotEmpty(t, got.feePayer)
		resultCount++
	}
	require.Equal(t, 3, resultCount)
}

func TestWrapperIsolation(t *testing.T) {
	addresses := testAddresses(t)
	require.NotEmpty(t, addresses)

	var expected wrapperResult
	for index, address := range addresses {
		connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		result, queryErr := queryWrapper(ctx, connection)
		cancel()
		require.NoError(t, connection.Close())
		require.NoError(t, queryErr)
		if index == 0 {
			expected = result
			continue
		}
		require.Equal(t, expected, result)
	}
}

func queryWrapper(ctx context.Context, connection *grpc.ClientConn) (wrapperResult, error) {
	health, err := grpcHealthV1.NewHealthClient(connection).Check(
		ctx,
		&grpcHealthV1.HealthCheckRequest{Service: queryServiceName},
	)
	if err != nil {
		return wrapperResult{}, err
	}
	if health.Status != grpcHealthV1.HealthCheckResponse_SERVING {
		return wrapperResult{}, fmt.Errorf("unexpected health status: %s", health.Status)
	}

	queryClient := query.NewQueryClient(connection)
	heightResponse, err := queryClient.GetMinaBlockHeight(ctx, &query.QueryGetMinaBlockHeightRequest{})
	if err != nil {
		return wrapperResult{}, err
	}
	actionsResponse, err := queryClient.GetActionsInRange(ctx, &query.QueryGetActionsInRangeRequest{
		StartBlockHeight: 10,
		EndBlockHeight:   12,
	})
	if err != nil {
		return wrapperResult{}, err
	}
	if len(actionsResponse.Actions) != 1 {
		return wrapperResult{}, fmt.Errorf("unexpected action count: %d", len(actionsResponse.Actions))
	}
	action := actionsResponse.Actions[0]
	if action.BlockHeight != 11 || action.ActionType != actions.ActionType_DEPOSIT || len(action.FeePayer) == 0 {
		return wrapperResult{}, errors.New("unexpected action payload")
	}

	return wrapperResult{
		height:   heightResponse.BlockHeight,
		amount:   action.Amount,
		feePayer: string(action.FeePayer),
	}, nil
}

func testAddresses(t *testing.T) []string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("ARCHIVE_WRAPPER_TEST_ADDRESSES"))
	require.NotEmpty(t, raw, "ARCHIVE_WRAPPER_TEST_ADDRESSES is required")
	return strings.Split(raw, ",")
}
