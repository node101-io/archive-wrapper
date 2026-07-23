package apperrors

import "errors"

// logger errors
var (
	ErrNilLogger = errors.New("nil logger")
)

// cmd errors
var (
	ErrPostgreUriRequired = errors.New("POSTGRES_URI is required")
)

// Database errors
var (
	ErrNilManager                      = errors.New("nil db manager")
	ErrUninitializedDB                 = errors.New("db manager is not initialized")
	ErrInvalidLenght                   = errors.New("invalid lenght")
	ErrBlockHeightMustBeBiggerThanZero = errors.New("block height must be bigger than zero")
	ErrInvalidKey                      = errors.New("invalid key")
)

// Mina Client Errors
var (
	ErrNilMinaClient          = errors.New("nil mina client")
	ErrNilConnection          = errors.New("nil connection")
	ErrInvalidContractAddress = errors.New("invalid contract address")
	ErrNilQueries             = errors.New("nil queries")
	ErrInvalidBlockHeight     = errors.New("invalid block height")
	ErrBestChainBlockNotFound = errors.New("best-chain block not found")

	ErrNilAction         = errors.New("nil action")
	ErrInvalidActionType = errors.New("invalid action type")
	ErrInvalidAmount     = errors.New("invalid action amount")
	ErrInvalidActionData = errors.New("invalid action data")
	ErrMissingFeePayer   = errors.New("missing fee payer")
)

// Indexer Errors
var (
	ErrNilIndexer                 = errors.New("nil indexer")
	ErrBlockHeightRegression      = errors.New("block height cursor cannot move backwards")
	ErrInvalidBlockRange          = errors.New("invalid block range")
	ErrNotificationConnectionLost = errors.New("notification connection lost")
	ErrStartBlockHeightMismatch   = errors.New("start block height does not match persisted start block height")
	ErrInvalidIndexedBounds       = errors.New("persisted index bounds are invalid")
)

// Config Errors
var (
	ErrGrpcAddressRequired       = errors.New("grpc_listen_address is required")
	ErrContractAddressRequired   = errors.New("contract_address is required")
	ErrBlockHeightDbKeyRequired  = errors.New("block_height_database_key is required")
	ErrDbPathRequired            = errors.New("db_path is required")
	ErrControlSocketPathRequired = errors.New("control socket path is required")
	ErrConfirmationDepthRequired = errors.New("confirmation_depth is required and must be greater than 0")
	ErrMaxActionRangeRequired    = errors.New("max_action_range_heights is required and must be greater than 0")
)
