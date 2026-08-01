package apperrors

import "errors"

var (
	// ErrNilLogger reports that a required logger dependency was not provided.
	ErrNilLogger = errors.New("nil logger")
)

var (
	// ErrPostgresURIRequired reports that the PostgreSQL connection string is missing.
	ErrPostgresURIRequired = errors.New("POSTGRES_URI is required")
)

var (
	// ErrNilManager reports that a required database manager dependency was not provided.
	ErrNilManager = errors.New("nil db manager")
	// ErrUninitializedDB reports that the database manager has no open LevelDB handle.
	ErrUninitializedDB = errors.New("db manager is not initialized")
	// ErrInvalidLength reports that an encoded block height has an invalid byte length.
	ErrInvalidLength = errors.New("invalid length")
	// ErrBlockHeightMustBeBiggerThanZero reports that a block height argument was non-positive.
	ErrBlockHeightMustBeBiggerThanZero = errors.New("block height must be bigger than zero")
	// ErrInvalidKey reports that a stored block record key does not match its action heights.
	ErrInvalidKey = errors.New("invalid key")
	// ErrDeploymentMetadataMissing reports indexed state that has no deployment identity.
	ErrDeploymentMetadataMissing = errors.New("deployment metadata is missing")
	// ErrDeploymentMetadataMismatch reports an attempt to reuse a DB for another deployment.
	ErrDeploymentMetadataMismatch = errors.New("deployment metadata does not match")
)

var (
	// ErrNilMinaClient reports that a required Mina client dependency was not provided.
	ErrNilMinaClient = errors.New("nil mina client")
	// ErrNilConnection reports that a required PostgreSQL notification connection was not provided.
	ErrNilConnection = errors.New("nil connection")
	// ErrInvalidContractAddress reports that the configured Mina zkApp address is invalid.
	ErrInvalidContractAddress = errors.New("invalid contract address")
	// ErrNilQueries reports that the archive SQL query dependency is missing.
	ErrNilQueries = errors.New("nil queries")
	// ErrInvalidBlockHeight reports that a block height argument is invalid for the attempted operation.
	ErrInvalidBlockHeight = errors.New("invalid block height")
	// ErrBestChainBlockNotFound reports that the selected best chain has no block at the requested height yet.
	ErrBestChainBlockNotFound = errors.New("best-chain block not found")
	// ErrQueryConnectionLost reports a retryable or timeout failure on archive query connections.
	ErrQueryConnectionLost = errors.New("query connection lost")

	// ErrNilAction reports that a nil action was encountered where a concrete action was required.
	ErrNilAction = errors.New("nil action")
	// ErrInvalidActionType reports that an action payload contains an unsupported type.
	ErrInvalidActionType = errors.New("invalid action type")
	// ErrInvalidAmount reports that an action amount is malformed or non-positive.
	ErrInvalidAmount = errors.New("invalid action amount")
	// ErrInvalidActionData reports that an action payload is missing required fields.
	ErrInvalidActionData = errors.New("invalid action data")
	// ErrMissingFeePayer reports that an action payload is missing the fee payer account.
	ErrMissingFeePayer = errors.New("missing fee payer")
)

var (
	// ErrNilIndexer reports that an expected indexer instance is nil.
	ErrNilIndexer = errors.New("nil indexer")
	// ErrBlockHeightRegression reports an attempt to move the latest processed cursor backwards.
	ErrBlockHeightRegression = errors.New("block height cursor cannot move backwards")
	// ErrInvalidBlockRange reports that the configured sync range parameters are invalid.
	ErrInvalidBlockRange = errors.New("invalid block range")
	// ErrNotificationConnectionLost reports a retryable PostgreSQL LISTEN/NOTIFY connection failure.
	ErrNotificationConnectionLost = errors.New("notification connection lost")
)

var (
	// ErrGRPCAddressRequired reports that grpc_listen_address is missing from config.
	ErrGRPCAddressRequired = errors.New("grpc_listen_address is required")
	// ErrContractAddressRequired reports that contract_address is missing from bridge params.
	ErrContractAddressRequired = errors.New("contract_address is required")
	// ErrBlockHeightDBKeyRequired reports that block_height_database_key is missing from config.
	ErrBlockHeightDBKeyRequired = errors.New("block_height_database_key is required")
	// ErrDBPathRequired reports that db_path is missing from config.
	ErrDBPathRequired = errors.New("db_path is required")
	// ErrDBAlreadyExists reports that start was asked to use an existing LevelDB path.
	ErrDBAlreadyExists = errors.New("db already exists")
	// ErrControlSocketPathRequired reports that the control socket path is missing from config.
	ErrControlSocketPathRequired = errors.New("control socket path is required")
	// ErrDeploymentMetadataKeyRequired reports that the deployment metadata key is missing from config.
	ErrDeploymentMetadataKeyRequired = errors.New("deployment metadata database key is required")
	// ErrDeploymentMetadataKeyConflict reports overlap with another LevelDB key namespace.
	ErrDeploymentMetadataKeyConflict = errors.New("deployment metadata database key conflicts with reserved keys")
	// ErrDeploymentSchemaVersionRequired reports that the deployment schema version is missing from config.
	ErrDeploymentSchemaVersionRequired = errors.New("deployment schema version is required")
	// ErrMinaNetworkIDRequired reports that mina_network_id is missing from config.
	ErrMinaNetworkIDRequired = errors.New("mina_network_id is required")
	// ErrConfirmationDepthRequired reports that confirmation_depth is missing or non-positive in bridge params.
	ErrConfirmationDepthRequired = errors.New("confirmation_depth is required and must be greater than 0")
	// ErrStartBlockHeightRequired reports that start_block_height is missing or non-positive in bridge params.
	ErrStartBlockHeightRequired = errors.New("start_block_height is required and must be greater than 0")
	// ErrMaxBlockRangeRequired reports that max_block_range is missing or non-positive in bridge params.
	ErrMaxBlockRangeRequired = errors.New("max_block_range is required and must be greater than 0")
)
