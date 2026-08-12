package fetchmina

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"sync"

	cosmosErrors "cosmossdk.io/errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/node101-io/mina-signer-go/address"

	minafield "github.com/node101-io/mina-signer-go/field"
)

const (
	actionTypeIndex        = 0
	actionXCoordinateIndex = 1
	actionIsOddIndex       = 2
	actionAmountIndex      = 3
	minimumActionFields    = actionAmountIndex + 1
)

// MinaClient reads best-chain block data and zkApp actions from the archive database.
type MinaClient struct {
	logger          *slog.Logger
	queries         sqlcdb.Querier
	contractAddress string
	cacheMu         sync.RWMutex
	bestChainCache  map[int64]int64
}

// NewMinaClient validates the configured contract address and query dependencies.
func NewMinaClient(contractAddress string, queries sqlcdb.Querier, logger *slog.Logger) (*MinaClient, error) {
	if logger == nil {
		return nil, apperrors.ErrNilLogger
	}
	logger = logger.With("component", "fetchmina")

	if queries == nil {
		return nil, apperrors.ErrNilQueries
	}

	contractAddress = strings.TrimSpace(contractAddress)
	if contractAddress == "" {
		return nil, apperrors.ErrInvalidContractAddress
	}

	// Validate once here so later queries can trust the configured address.
	_, err := address.NewAddress(contractAddress).Marshal()
	if err != nil {
		return nil, err
	}

	logger.Info("mina client initialized", "contract_address", contractAddress)

	return &MinaClient{
		logger:          logger,
		queries:         queries,
		contractAddress: contractAddress,
	}, nil
}

// GetMinaBlockHeight returns the latest block height visible in the archive database.
func (c *MinaClient) GetMinaBlockHeight(ctx context.Context) (int64, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}

	height, err := c.queries.GetLatestBlockHeight(ctx)
	if err != nil {
		return 0, wrapQueryError("query latest block height", err)
	}

	c.logger.InfoContext(ctx, "fetched latest mina block height", "height", height)

	return height, nil
}

// FetchActions returns zkApp actions for blockHeight on the cached best chain.
// It returns ErrBestChainBlockNotFound when the selected chain has no block at that height yet.
func (c *MinaClient) FetchActions(ctx context.Context, blockHeight int64) ([]actions.Action, error) {
	if blockHeight <= 0 {
		return nil, cosmosErrors.Wrap(apperrors.ErrInvalidBlockHeight, "block height must be greater than 0")
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	blockID, err := c.bestChainBlockIDForHeight(ctx, blockHeight)
	if err != nil {
		return nil, err
	}
	if blockID == 0 {
		return nil, fmt.Errorf("%w: height %d resolved to zero block id", apperrors.ErrBestChainBlockNotFound, blockHeight)
	}

	rows, err := c.queries.ListActionRowsByBlockID(ctx, sqlcdb.ListActionRowsByBlockIDParams{
		BlockID:         blockID,
		ContractAddress: c.contractAddress,
	})
	if err != nil {
		return nil, wrapQueryError("query archive actions", err)
	}

	result := make([]actions.Action, 0)
	for _, row := range rows {
		// Empty payload means there is nothing usable to index from this row.
		if len(row.Data) == 0 {
			continue
		}

		action, err := actionFromRawData(row.Height, row.Data)
		if err != nil {
			return nil, err
		}

		result = append(result, *action)
	}

	c.logger.InfoContext(
		ctx,
		"fetched actions",
		"block_height",
		blockHeight,
		"rows",
		len(rows),
		"actions",
		len(result),
	)

	return result, nil
}

// PrimeBestChainRange caches best-chain block IDs for the inclusive height range.
func (c *MinaClient) PrimeBestChainRange(ctx context.Context, startHeight, endHeight int64) error {
	if startHeight <= 0 {
		return cosmosErrors.Wrap(apperrors.ErrInvalidBlockHeight, "start height must be greater than 0")
	}
	if endHeight < startHeight {
		return nil
	}

	if err := c.validate(); err != nil {
		return err
	}

	rows, err := c.queries.ListBestChainBlockIDsInRange(ctx, sqlcdb.ListBestChainBlockIDsInRangeParams{
		StartHeight: startHeight,
		EndHeight:   endHeight,
	})
	if err != nil {
		return wrapQueryError("query best chain range", err)
	}

	blockIDs := make(map[int64]int64, len(rows))
	for _, row := range rows {
		blockIDs[row.Height] = row.ID
	}

	c.cacheMu.Lock()
	c.bestChainCache = blockIDs
	c.cacheMu.Unlock()

	c.logger.InfoContext(
		ctx,
		"primed best chain range",
		"start_height",
		startHeight,
		"end_height",
		endHeight,
		"blocks",
		len(blockIDs),
	)

	return nil
}

func (c *MinaClient) validate() error {
	if c == nil || c.queries == nil {
		return apperrors.ErrNilMinaClient
	}

	if c.logger == nil {
		return apperrors.ErrNilLogger
	}

	if strings.TrimSpace(c.contractAddress) == "" {
		return apperrors.ErrInvalidContractAddress
	}

	return nil
}

func (c *MinaClient) bestChainBlockIDForHeight(ctx context.Context, blockHeight int64) (int64, error) {
	c.cacheMu.RLock()
	blockID, ok := c.bestChainCache[blockHeight]
	c.cacheMu.RUnlock()
	if ok {
		return blockID, nil
	}

	row, err := c.queries.GetBestChainBlockIDAtHeight(ctx, blockHeight)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("%w: height %d", apperrors.ErrBestChainBlockNotFound, blockHeight)
		}
		return 0, wrapQueryError("query best chain block id", err)
	}

	c.cacheMu.Lock()
	if c.bestChainCache == nil {
		c.bestChainCache = make(map[int64]int64)
	}
	c.bestChainCache[blockHeight] = row.ID
	c.cacheMu.Unlock()

	return row.ID, nil
}

func actionFromRawData(blockHeight int64, data []string) (*actions.Action, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) < minimumActionFields {
		return nil, cosmosErrors.Wrapf(
			apperrors.ErrInvalidActionData,
			"action payload has %d fields; expected at least %d",
			len(data),
			minimumActionFields,
		)
	}

	actionType, amount, err := parseActionData(data)
	if err != nil {
		return nil, err
	}

	xCoordinateBytes, err := fieldBytesFromDecimal(data[actionXCoordinateIndex])
	if err != nil {
		return nil, err
	}

	isOdd := parseIsOddField(data[actionIsOddIndex])

	return &actions.Action{
		BlockHeight: blockHeight,
		XCoordinate: xCoordinateBytes,
		IsOdd:       isOdd,
		ActionType:  actionType,
		Amount:      amount,
	}, nil
}

func fieldBytesFromDecimal(s string) ([]byte, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 {
		return nil, cosmosErrors.Wrap(
			apperrors.ErrInvalidActionData,
			"invalid account x_coordinate",
		)
	}

	raw := n.Bytes()
	size := minafield.NewField().ElementSize()

	// Oversized/non-canonical values are forwarded for Pulsar to reject.
	if len(raw) >= size {
		return raw, nil
	}

	// Preserve the existing 32-byte wire representation for valid values.
	b := make([]byte, size)
	copy(b[size-len(raw):], raw)

	return b, nil
}

func parseIsOddField(v string) bool {
	return v == "1"
}

func wrapQueryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, pgconn.ErrConnClosed) || pgconn.SafeToRetry(err) || pgconn.Timeout(err) {
		return fmt.Errorf("%w: %s: %w", apperrors.ErrQueryConnectionLost, operation, err)
	}

	return fmt.Errorf("%s: %w", operation, err)
}

func parseActionData(data []string) (actions.ActionType, int64, error) {
	if len(data) == 0 {
		return 0, 0, cosmosErrors.Wrap(apperrors.ErrInvalidActionData, "missing action type")
	}

	actionTypeValue, err := strconv.ParseInt(data[actionTypeIndex], 10, 32)
	if err != nil {
		return 0, 0, apperrors.ErrInvalidActionType
	}

	amount, err := strconv.ParseInt(data[actionAmountIndex], 10, 64)
	if err != nil {
		return 0, 0, apperrors.ErrInvalidAmount
	}

	return actions.ActionType(actionTypeValue), amount, nil
}
