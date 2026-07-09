package fetchmina

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	cosmosErrors "cosmossdk.io/errors"
	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/node101-io/mina-signer-go/address"
)

const (
	actionTypeIndex     = 0
	actionAmountIndex   = 3
	minimumActionFields = actionAmountIndex + 1
)

type MinaClient struct {
	logger          *slog.Logger
	queries         *sqlcdb.Queries
	contractAddress string
}

func NewMinaClient(contractAddress string, queries *sqlcdb.Queries, logger *slog.Logger) (*MinaClient, error) {
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

func (c *MinaClient) GetMinaBlockHeight(ctx context.Context) (int64, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}

	height, err := c.queries.GetLatestBlockHeight(ctx)
	if err != nil {
		return 0, cosmosErrors.Wrap(err, "err at query latest block height")
	}

	c.logger.InfoContext(ctx, "fetched latest mina block height", "height", height)

	return height, nil
}

func (c *MinaClient) FetchActions(ctx context.Context, blockHeight int64) ([]actions.Action, error) {
	if blockHeight <= 0 {
		return nil, cosmosErrors.Wrap(apperrors.ErrInvalidBlockHeight, "block height must be greater than 0")
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	rows, err := c.queries.ListActionRows(ctx, sqlcdb.ListActionRowsParams{
		ContractAddress: c.contractAddress,
		Height:          blockHeight,
	})
	if err != nil {
		return nil, cosmosErrors.Wrap(err, "err at query archive actions")
	}

	result := make([]actions.Action, 0)
	for _, row := range rows {
		if len(row.Data) == 0 {
			continue
		}

		action, err := actionFromRawData(row.Height, row.FeePayer, row.Data)
		if err != nil {
			return nil, err
		}
		if action == nil {
			continue
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

func actionFromRawData(blockHeight int64, feePayer string, data []string) (*actions.Action, error) {
	if len(data) == 0 {
		return nil, nil
	}

	actionTypeValue, err := strconv.Atoi(data[actionTypeIndex])
	if err != nil {
		return nil, err
	}

	switch actions.ActionType(actionTypeValue) {
	case actions.ActionType_UNSPECIFIED:
		return nil, nil
	case actions.ActionType_DEPOSIT, actions.ActionType_WITHDRAW:
	default:
		return nil, nil
	}

	actionType, amount, err := parseActionData(data)
	if err != nil {
		return nil, err
	}
	if actionType == actions.ActionType_UNSPECIFIED {
		return nil, nil
	}

	minaAddr, err := address.NewAddress(feePayer).Marshal()
	if err != nil {
		return nil, err
	}

	return &actions.Action{
		BlockHeight: blockHeight,
		FeePayer:    minaAddr,
		ActionType:  actionType,
		Amount:      amount,
	}, nil
}

func parseActionData(data []string) (actions.ActionType, int64, error) {
	if len(data) == 0 {
		return 0, 0, cosmosErrors.Wrap(apperrors.ErrInvalidActionData, "missing action type")
	}

	actionTypeValue, err := strconv.Atoi(data[actionTypeIndex])
	if err != nil {
		return 0, 0, apperrors.ErrInvalidActionType
	}

	switch actionTypeValue {
	case int(actions.ActionType_UNSPECIFIED):
		return actions.ActionType_UNSPECIFIED, 0, nil

	case int(actions.ActionType_DEPOSIT), int(actions.ActionType_WITHDRAW):
		if len(data) < minimumActionFields {
			return 0, 0, cosmosErrors.Wrap(apperrors.ErrInvalidActionData, "missing fields")
		}

		amount, err := strconv.ParseInt(data[actionAmountIndex], 10, 64)
		if err != nil {
			return 0, 0, apperrors.ErrInvalidAmount
		}
		if amount <= 0 {
			return 0, 0, cosmosErrors.Wrap(apperrors.ErrInvalidAmount, "non-positive amount")
		}

		return actions.ActionType(actionTypeValue), amount, nil

	default:
		return 0, 0, apperrors.ErrInvalidActionType
	}
}
