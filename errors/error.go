package errors

import "errors"

var (
	ErrNilManager      = errors.New("nil manager")
	ErrUninitializedDB = errors.New("db manager is not initialized")

	ErrBlockHeightMustBeBiggerThanZero = errors.New("block height must be bigger than zero")

	ErrInvalidKey = errors.New("invalid key")

	ErrAmountMustBeBiggerThanZero = errors.New("amount must be bigger than zero")

	ErrNilAction = errors.New("nil action")

	ErrInvalidActionType = errors.New("invalid action type")
	ErrInvalidAmount     = errors.New("invalid action amount")
	ErrInvalidActionData = errors.New("invalid action data")
	ErrMissingFeePayer   = errors.New("missing fee payer")

	ErrInvalidBlockRange = errors.New("invalid block range")

	ErrNilMinaClient      = errors.New("nil mina client")
	ErrInvalidBlockHeight = errors.New("invalid block height")

	ErrNoActionsInBlock = errors.New("no actions in this block")
)
