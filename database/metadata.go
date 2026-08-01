package database

import (
	"encoding/json"
	"errors"
	"fmt"

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

// EnsureDeploymentMetadata initializes an empty database or validates that an
// existing database belongs to the same deployment.
func (manager *DbManager) EnsureDeploymentMetadata(
	metadataKey string,
	expected DeploymentMetadata,
) error {
	if err := manager.Validate(); err != nil {
		return err
	}

	manager.blockHeightMu.Lock()
	defer manager.blockHeightMu.Unlock()

	key := []byte(metadataKey)
	record, err := manager.db.Get(key, nil)
	switch {
	case err == nil:
		var stored DeploymentMetadata
		if err := json.Unmarshal(record, &stored); err != nil {
			return fmt.Errorf("unmarshal deployment metadata: %w", err)
		}
		if stored != expected {
			return fmt.Errorf(
				"%w: stored=%+v requested=%+v",
				apperrors.ErrDeploymentMetadataMismatch,
				stored,
				expected,
			)
		}
		return nil

	case errors.Is(err, leveldb.ErrNotFound):
		hasCursor, err := manager.db.Has([]byte(manager.blockHeightDatabaseKey), nil)
		if err != nil {
			return fmt.Errorf("check block height cursor: %w", err)
		}

		_, hasRecord, err := manager.getEarliestStoredRecordHeight()
		if err != nil {
			return err
		}
		if hasCursor || hasRecord {
			return apperrors.ErrDeploymentMetadataMissing
		}

		encoded, err := json.Marshal(expected)
		if err != nil {
			return fmt.Errorf("marshal deployment metadata: %w", err)
		}
		return manager.db.Put(key, encoded, nil)

	default:
		return fmt.Errorf("get deployment metadata: %w", err)
	}
}
