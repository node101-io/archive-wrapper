package diagnostics

const (
	// StateUnspecified represents an unset operational state.
	StateUnspecified OperationalState = OperationalState_OPERATIONAL_STATE_UNSPECIFIED
	// StateStarting indicates that runtime initialization is in progress.
	StateStarting OperationalState = OperationalState_OPERATIONAL_STATE_STARTING
	// StateConnecting indicates that PostgreSQL connectivity is being probed.
	StateConnecting OperationalState = OperationalState_OPERATIONAL_STATE_CONNECTING
	// StateSyncing indicates that archive reconciliation is in progress.
	StateSyncing OperationalState = OperationalState_OPERATIONAL_STATE_SYNCING
	// StateWaitingForFinality indicates that no finalized cursor is available yet.
	StateWaitingForFinality OperationalState = OperationalState_OPERATIONAL_STATE_WAITING_FOR_FINALITY
	// StateReady indicates that business queries are available.
	StateReady OperationalState = OperationalState_OPERATIONAL_STATE_READY
	// StateReconnecting indicates recovery from a PostgreSQL connection failure.
	StateReconnecting OperationalState = OperationalState_OPERATIONAL_STATE_RECONNECTING
	// StateFailed indicates a terminal runtime failure.
	StateFailed OperationalState = OperationalState_OPERATIONAL_STATE_FAILED
	// StateStopping indicates terminal shutdown is in progress.
	StateStopping OperationalState = OperationalState_OPERATIONAL_STATE_STOPPING
	// StateWaitingForArchive indicates that the archive target is behind the persisted cursor.
	StateWaitingForArchive OperationalState = OperationalState_OPERATIONAL_STATE_WAITING_FOR_ARCHIVE
)
