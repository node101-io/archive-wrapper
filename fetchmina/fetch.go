package fetchmina

import (
	"context"
	"strconv"

	"github.com/node101-io/archive-wrapper/types"

	actions "github.com/node101-io/archive-wrapper/actions"

	"github.com/node101-io/archive-wrapper/apperrors"

	cosmosErrors "cosmossdk.io/errors"
	"github.com/Khan/genqlient/graphql"
	"github.com/node101-io/mina-signer-go/address"
)

type MinaClient struct {
	client graphql.Client
}

func NewMinaClient(client graphql.Client, ctx context.Context) *MinaClient {
	return &MinaClient{
		client: client,
	}
}

const (
	actionTypeIndex     = 0
	actionAmountIndex   = 3
	minimumActionFields = actionAmountIndex + 1
)

func (c *MinaClient) GetMinaBlockHeight(ctx context.Context) (int64, error) {

	resp, err := MinaBlockHeight(ctx, c.client)
	if err != nil {
		return 0, err
	}

	return int64(resp.NetworkState.MaxBlockHeight.PendingMaxBlockHeight), nil
}

func (c *MinaClient) FetchActions(ctx context.Context, start, end int) ([]actions.Action, error) {

	if start > end {
		return nil, cosmosErrors.Wrap(apperrors.ErrInvalidBlockRange, "start is bigger than end")

	}

	resp, err := MinaArchiveActions(
		ctx,
		c.client,
		types.ContractAddress,
		start,
		end,
		start,
		end+1,
		end-start+1,
	)
	if err != nil {
		return nil, err
	}

	feePayerByHash := make(map[string]string)
	for _, block := range resp.Blocks {
		for _, command := range block.Transactions.ZkappCommands {
			feePayerByHash[command.Hash] = command.FeePayer
		}
	}

	actions := make([]actions.Action, 0)
	for _, group := range resp.Actions {
		for _, raw := range group.ActionData {
			feePayer, ok := feePayerByHash[raw.TransactionInfo.Hash]
			if !ok {
				return nil, cosmosErrors.Wrap(apperrors.ErrMissingFeePayer, raw.TransactionInfo.Hash)
			}

			action, err := actionFromRawData((group.BlockInfo.Height), feePayer, raw.Data)
			if err != nil {
				return nil, err
			}
			if action == nil {
				continue
			}

			actions = append(actions, *action)
		}
	}

	return actions, nil
}

func actionFromRawData(blockHeight int, feePayer string, data []string) (*actions.Action, error) {

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
		BlockHeight: int64(blockHeight),
		FeePayer:    minaAddr,
		ActionType:  actionType,
		Amount:      amount,
	}, nil
}

func parseActionData(data []string) (actions.ActionType, int64, error) {

	if len(data) < minimumActionFields {
		return 0, 0, cosmosErrors.Wrap(apperrors.ErrInvalidActionData, "missing fields")
	}

	actionTypeValue, err := strconv.Atoi(data[actionTypeIndex])
	if err != nil {
		return 0, 0, apperrors.ErrInvalidActionType
	}

	switch actionTypeValue {
	case int(actions.ActionType_UNSPECIFIED):
		return actions.ActionType_UNSPECIFIED, 0, nil

	case int(actions.ActionType_DEPOSIT), int(actions.ActionType_WITHDRAW):

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
