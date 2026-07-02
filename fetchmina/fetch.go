package fetchmina

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	cosmosErrors "cosmossdk.io/errors"
	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	sqlcdb "github.com/node101-io/archive-wrapper/fetchmina/db"
	"github.com/node101-io/mina-signer-go/address"

	"github.com/jackc/pgx/v5"
)

const (
	actionTypeIndex     = 0
	actionAmountIndex   = 3
	minimumActionFields = actionAmountIndex + 1
)

type MinaClient struct {
	conn            *pgx.Conn
	queries         *sqlcdb.Queries
	contractAddress string
}

func NewMinaClient(postgresURI string, ctx context.Context) (*MinaClient, error) {
	if strings.TrimSpace(postgresURI) == "" {
		postgresURI = readPostgresURI()
	}

	if strings.TrimSpace(postgresURI) == "" {
		return nil, fmt.Errorf("postgres uri is empty")
	}

	conn, err := pgx.Connect(ctx, postgresURI)
	if err != nil {
		return nil, fmt.Errorf("connect to archive db: %w", err)
	}

	return &MinaClient{
		conn:    conn,
		queries: sqlcdb.New(conn),
	}, nil
}

func (c *MinaClient) Close(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return nil
	}

	return c.conn.Close(ctx)
}

func (c *MinaClient) GetMinaBlockHeight(ctx context.Context) (int64, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}

	height, err := c.queries.GetLatestBlockHeight(ctx)
	if err != nil {
		return 0, fmt.Errorf("query latest block height: %w", err)
	}

	return height, nil
}

func (c *MinaClient) FetchActions(ctx context.Context, start, end int) ([]actions.Action, error) {
	if start > end {
		return nil, cosmosErrors.Wrap(apperrors.ErrInvalidBlockRange, "start is bigger than end")
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	rows, err := c.queries.ListActionRows(ctx, sqlcdb.ListActionRowsParams{
		ContractAddress:    c.contractAddress,
		StartHeight:        int64(start),
		EndHeightExclusive: int64(end + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("query archive actions: %w", err)
	}

	result := make([]actions.Action, 0)
	for _, row := range rows {
		if len(row.Data) == 0 {
			continue
		}

		action, err := actionFromRawData(int(row.Height), row.FeePayer, row.Data)
		if err != nil {
			return nil, err
		}
		if action == nil {
			continue
		}

		result = append(result, *action)
	}

	return result, nil
}

func (c *MinaClient) validate() error {
	if c == nil || c.conn == nil || c.queries == nil {
		return fmt.Errorf("archive db connection is not initialized")
	}

	if strings.TrimSpace(c.contractAddress) == "" {
		return fmt.Errorf("contract address is empty")
	}

	return nil
}

func actionFromRawData(blockHeight int, feePayer string, data []string) (*actions.Action, error) {
	if len(data) == 0 {
		return nil, nil
	}

	actionTypeValue, err := strconv.Atoi(data[actionTypeIndex])
	if err != nil {
		return nil, nil
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
		BlockHeight: int64(blockHeight),
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

func readPostgresURI() string {
	if value := strings.TrimSpace(os.Getenv("POSTGRES_URI")); value != "" {
		return value
	}

	return strings.TrimSpace(os.Getenv("ARCHIVE_DATABASE_URL"))
}
