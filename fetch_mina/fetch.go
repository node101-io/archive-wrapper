package fetchmina

import (
	"archive-wrapper/types"
	"context"
	"strconv"

	"archive-wrapper/errors"

	cosmosErrors "cosmossdk.io/errors"
	"github.com/Khan/genqlient/graphql"
	"github.com/node101-io/mina-signer-go/address"
)

type MinaClient struct {
	client graphql.Client
	ctx    context.Context
}

func NewMinaClient(client graphql.Client, ctx context.Context) *MinaClient {
	return &MinaClient{
		client: client,
		ctx:    ctx,
	}
}

const (
	actionTypeIndex     = 0
	actionAmountIndex   = 3
	minimumActionFields = actionAmountIndex + 1
)

func (c *MinaClient) GetMinaBlockHeight() (int64, error) {

	resp, err := MinaBlockHeight(c.ctx, c.client)
	if err != nil {
		return 0, err
	}

	return int64(resp.NetworkState.MaxBlockHeight.PendingMaxBlockHeight), nil
}

func (c *MinaClient) FetchActions(start, end int) ([]types.Action, error) {

	if start > end {
		return nil, cosmosErrors.Wrap(errors.ErrInvalidBlockRange, "start is bigger than end")

	}

	resp, err := MinaArchiveActions(
		c.ctx,
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

	actions := make([]types.Action, 0)
	for _, group := range resp.Actions {
		for _, raw := range group.ActionData {
			feePayer, ok := feePayerByHash[raw.TransactionInfo.Hash]
			if !ok {
				return nil, cosmosErrors.Wrap(errors.ErrMissingFeePayer, raw.TransactionInfo.Hash)
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

func actionFromRawData(blockHeight int, feePayer string, data []string) (*types.Action, error) {

	actionType, amount, err := parseActionData(data)
	if err != nil {
		return nil, err
	}
	if actionType == types.ActionType_UNSPECIFIED {
		return nil, nil
	}

	minaAddr, err := address.NewAddress(feePayer).Marshal()
	if err != nil {
		return nil, err
	}

	return &types.Action{
		BlockHeight: int64(blockHeight),
		FeePayer:    minaAddr,
		ActionType:  actionType,
		Amount:      amount,
	}, nil
}

func parseActionData(data []string) (types.ActionType, int64, error) {

	if len(data) < minimumActionFields {
		return 0, 0, cosmosErrors.Wrap(errors.ErrInvalidActionData, "missing fields")
	}

	actionTypeValue, err := strconv.Atoi(data[actionTypeIndex])
	if err != nil {
		return 0, 0, errors.ErrInvalidActionType
	}

	if actionTypeValue == int(types.ActionType_UNSPECIFIED) {
		return types.ActionType_UNSPECIFIED, 0, cosmosErrors.Wrap(errors.ErrInvalidActionType, "unspecified action type")
	}

	amount, err := strconv.ParseInt(data[actionAmountIndex], 10, 64)
	if err != nil {
		return 0, 0, errors.ErrInvalidAmount
	}
	if amount <= 0 {
		return 0, 0, cosmosErrors.Wrap(errors.ErrInvalidAmount, "non-positive amount")
	}

	return types.ActionType(actionTypeValue), amount, nil
}
