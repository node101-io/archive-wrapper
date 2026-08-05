package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/syndtr/goleveldb/leveldb"
)

// DeploymentMetadata identifies the history stored in a LevelDB database.
type DeploymentMetadata struct {
	SchemaVersion   uint32 `mapstructure:"schema_version" json:"schema_version"`
	MinaNetworkID   string `mapstructure:"mina_network_id" json:"mina_network_id"`
	ContractAddress string `json:"contract_address"`
	StartHeight     int64  `json:"start_height"`
}

// DeploymentState classifies whether a valid wrapper database was initialized now or earlier.
type DeploymentState uint8

const (
	DeploymentStateUnspecified DeploymentState = iota
	DeploymentStateFresh
	DeploymentStateInitialized
)

func (state DeploymentState) String() string {
	switch state {
	case DeploymentStateFresh:
		return "fresh"
	case DeploymentStateInitialized:
		return "initialized"
	default:
		return "unspecified"
	}
}

// InitializeOrValidateDeployment initializes an empty application keyspace or
// validates that persisted state belongs to the expected deployment.
func (manager *DbManager) InitializeOrValidateDeployment(
	metadataKey string,
	expected DeploymentMetadata,
) (DeploymentState, error) {
	if err := manager.Validate(); err != nil {
		return DeploymentStateUnspecified, err
	}
	if err := validateDeploymentMetadata(expected); err != nil {
		return DeploymentStateUnspecified, err
	}

	manager.blockHeightMu.Lock()
	defer manager.blockHeightMu.Unlock()

	key := []byte(metadataKey)
	record, err := manager.db.Get(key, nil)
	switch {
	case err == nil:
		stored, err := decodeDeploymentMetadata(record)
		if err != nil {
			return DeploymentStateUnspecified, fmt.Errorf("%w: decode deployment metadata: %w", apperrors.ErrDBCorrupt, err)
		}
		if err := validateDeploymentMetadata(stored); err != nil {
			return DeploymentStateUnspecified, fmt.Errorf("%w: validate deployment metadata: %w", apperrors.ErrDBCorrupt, err)
		}
		if stored != expected {
			return DeploymentStateUnspecified, fmt.Errorf(
				"%w: stored=%+v requested=%+v",
				apperrors.ErrDeploymentMetadataMismatch,
				stored,
				expected,
			)
		}
		if err := manager.validatePersistedCursor(expected.StartHeight); err != nil {
			return DeploymentStateUnspecified, err
		}
		return DeploymentStateInitialized, nil

	case errors.Is(err, leveldb.ErrNotFound):
		hasApplicationState, err := manager.hasApplicationState()
		if err != nil {
			return DeploymentStateUnspecified, err
		}
		if hasApplicationState {
			return DeploymentStateUnspecified, apperrors.ErrDBStateIncomplete
		}

		encoded, err := json.Marshal(expected)
		if err != nil {
			return DeploymentStateUnspecified, fmt.Errorf("marshal deployment metadata: %w", err)
		}
		if err := manager.db.Put(key, encoded, nil); err != nil {
			return DeploymentStateUnspecified, wrapDatabaseError("store deployment metadata", err)
		}
		return DeploymentStateFresh, nil

	default:
		return DeploymentStateUnspecified, wrapDatabaseError("get deployment metadata", err)
	}
}

func decodeDeploymentMetadata(record []byte) (DeploymentMetadata, error) {
	decoder := json.NewDecoder(bytes.NewReader(record))
	decoder.DisallowUnknownFields()

	var metadata DeploymentMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return DeploymentMetadata{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return DeploymentMetadata{}, errors.New("multiple JSON values")
		}
		return DeploymentMetadata{}, err
	}

	return metadata, nil
}

func validateDeploymentMetadata(metadata DeploymentMetadata) error {
	if metadata.SchemaVersion == 0 {
		return apperrors.ErrDeploymentSchemaVersionRequired
	}
	if strings.TrimSpace(metadata.MinaNetworkID) == "" {
		return apperrors.ErrMinaNetworkIDRequired
	}
	if strings.TrimSpace(metadata.ContractAddress) == "" {
		return apperrors.ErrContractAddressRequired
	}
	if metadata.StartHeight <= 0 {
		return apperrors.ErrStartBlockHeightRequired
	}
	return nil
}

func (manager *DbManager) validatePersistedCursor(startHeight int64) error {
	record, err := manager.db.Get([]byte(manager.blockHeightDatabaseKey), nil)
	switch {
	case err == nil:
		height, err := decodeBlockHeight(record)
		if err != nil {
			return fmt.Errorf("%w: decode block height cursor: %w", apperrors.ErrDBCorrupt, err)
		}
		if height <= 0 {
			return fmt.Errorf(
				"%w: block height cursor must be positive: %d",
				apperrors.ErrDBCorrupt,
				height,
			)
		}
		if height < startHeight {
			return fmt.Errorf(
				"%w: block height cursor %d is below deployment start height %d",
				apperrors.ErrDBCorrupt,
				height,
				startHeight,
			)
		}
		return nil
	case errors.Is(err, leveldb.ErrNotFound):
		return nil
	default:
		return wrapDatabaseError("get block height cursor", err)
	}
}

func (manager *DbManager) hasApplicationState() (bool, error) {
	iter := manager.db.NewIterator(nil, nil)
	defer iter.Release()

	hasState := iter.First()
	if err := iter.Error(); err != nil {
		return false, wrapDatabaseError("iterate application state", err)
	}
	return hasState, nil
}
