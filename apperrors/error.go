package apperrors

import "errors"

var (
	ErrNilManager      = errors.New("nil db manager")
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

	ErrNilQuery       = errors.New("nil query")
	ErrInvalidRequest = errors.New("invalid query request")

	ErrInvalidLenght = errors.New("invalid lenght")

	ErrNilConnection          = errors.New("nil connection")
	ErrInvalidContractAddress = errors.New("invalid contract address")
	ErrNilQueries             = errors.New("nil queries")

	ErrNilIndexer            = errors.New("nil indexer")
	ErrBlockHeightRegression = errors.New("block height cursor cannot move backwards")

	ErrGrpcAddressRequired       = errors.New("grpc_listen_address is required")
	ErrContractAddressRequired   = errors.New("contract_address is required")
	ErrBlockHeightDbKeyRequired  = errors.New("block_height_database_key is required")
	ErrDbPathRequired            = errors.New("db_path is required")
	ErrControlSocketPathRequired = errors.New("control socket path is required")
	ErrConfirmationDepthRequired = errors.New("confirmation_depth is required and must be greater than 0")
)
