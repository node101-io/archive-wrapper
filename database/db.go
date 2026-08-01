package database

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/node101-io/archive-wrapper/apperrors"

	"fmt"

	actions "github.com/node101-io/archive-wrapper/actions"

	proto "github.com/cosmos/gogoproto/proto"
	"github.com/syndtr/goleveldb/leveldb"
)

// DbManager stores indexed block records and cursor metadata in LevelDB.
type DbManager struct {
	logger                 *slog.Logger
	db                     *leveldb.DB
	blockHeightDatabaseKey string
	blockHeightMu          sync.Mutex
}

// NewDbManager opens or creates a LevelDB store at path.
// Callers must Close the returned manager when they are done with it.
func NewDbManager(path, blockHeightDatabaseKey string, logger *slog.Logger) (*DbManager, error) {
	if logger == nil {
		return nil, apperrors.ErrNilLogger
	}
	logger = logger.With("component", "database", "path", path)

	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, fmt.Errorf("open leveldb: %w", err)
	}

	logger.Info("leveldb opened")

	return &DbManager{
		logger:                 logger,
		db:                     db,
		blockHeightDatabaseKey: blockHeightDatabaseKey,
	}, nil
}

// Validate reports whether the manager is ready to serve reads and writes.
func (manager *DbManager) Validate() error {

	if manager == nil {
		return apperrors.ErrNilManager
	}

	if manager.db == nil {
		return apperrors.ErrUninitializedDB
	}

	if manager.logger == nil {
		return apperrors.ErrNilLogger
	}

	return nil
}

// Insert stores the indexed actions for a single block height.
func (manager *DbManager) Insert(record actions.DbRecord) error {

	if err := manager.Validate(); err != nil {
		return err
	}

	if err := validateRecord(record); err != nil {
		return err
	}

	marshalled, err := proto.Marshal(&record)
	if err != nil {
		return err
	}

	if manager.logger != nil {
		manager.logger.Debug("storing block record", "height", record.Key, "actions", len(record.Actions))
	}

	return manager.db.Put(encodeBlockHeight(record.Key), marshalled, nil)
}

// CommitBlock atomically stores an action-bearing block and advances the cursor.
func (manager *DbManager) CommitBlock(record actions.DbRecord) error {
	if err := manager.Validate(); err != nil {
		return err
	}
	if err := validateRecord(record); err != nil {
		return err
	}

	marshalled, err := proto.Marshal(&record)
	if err != nil {
		return err
	}

	manager.blockHeightMu.Lock()
	defer manager.blockHeightMu.Unlock()

	cursorKey := []byte(manager.blockHeightDatabaseKey)
	cursorRecord, err := manager.db.Get(cursorKey, nil)
	switch {
	case err == nil:
		currentHeight, err := decodeBlockHeight(cursorRecord)
		if err != nil {
			return err
		}
		if record.Key < currentHeight {
			return fmt.Errorf(
				"%w: current=%d requested=%d",
				apperrors.ErrBlockHeightRegression,
				currentHeight,
				record.Key,
			)
		}
	case errors.Is(err, leveldb.ErrNotFound):
	default:
		return fmt.Errorf("get block height cursor: %w", err)
	}

	// One batch prevents restart from observing a record without its cursor.
	batch := new(leveldb.Batch)
	batch.Put(encodeBlockHeight(record.Key), marshalled)
	batch.Put(cursorKey, encodeBlockHeight(record.Key))
	return manager.db.Write(batch, nil)
}

// Has reports whether a block record exists for height.
func (manager *DbManager) Has(height int64) (bool, error) {

	if err := manager.Validate(); err != nil {
		return false, err
	}

	exists, err := manager.db.Has(encodeBlockHeight(height), nil)
	if err != nil {
		return false, err
	}

	return exists, err
}

// Get loads the indexed block record stored for height.
func (manager *DbManager) Get(height int64) (actions.DbRecord, error) {

	if err := manager.Validate(); err != nil {
		return actions.DbRecord{}, err
	}

	marshalled, err := manager.db.Get(encodeBlockHeight(height), nil)
	if err != nil {
		return actions.DbRecord{}, err
	}

	var record actions.DbRecord
	err = proto.Unmarshal(marshalled, &record)
	if err != nil {
		return actions.DbRecord{}, err
	}

	if manager.logger != nil {
		manager.logger.Debug("loaded block record", "height", height, "actions", len(record.Actions))
	}

	return record, nil
}

// InsertBlockHeight advances the latest processed block cursor.
// Duplicate heights are treated as idempotent updates and regressions are rejected.
func (manager *DbManager) InsertBlockHeight(height int64) error {

	if err := manager.Validate(); err != nil {
		return err
	}

	if height <= 0 {
		return apperrors.ErrBlockHeightMustBeBiggerThanZero
	}

	manager.blockHeightMu.Lock()
	defer manager.blockHeightMu.Unlock()

	key := []byte(manager.blockHeightDatabaseKey)

	record, err := manager.db.Get(key, nil)
	switch {
	case err == nil:
		currentHeight, err := decodeBlockHeight(record)
		if err != nil {
			return err
		}

		if height < currentHeight {
			return fmt.Errorf(
				"%w: current=%d requested=%d",
				apperrors.ErrBlockHeightRegression,
				currentHeight,
				height,
			)
		}

		// Duplicate update idempotent olsun.
		if height == currentHeight {
			if manager.logger != nil {
				manager.logger.Debug("block height cursor already up to date", "height", height)
			}
			return nil
		}

	case errors.Is(err, leveldb.ErrNotFound):
		// İlk cursor yazımı.

	default:
		return fmt.Errorf("get block height cursor: %w", err)
	}

	if manager.logger != nil {
		manager.logger.Debug("updating block height cursor", "height", height)
	}

	return manager.db.Put(key, encodeBlockHeight(height), nil)
}

// HasBlockHeight reports whether the latest processed block cursor exists.
func (manager *DbManager) HasBlockHeight() (bool, error) {
	if err := manager.Validate(); err != nil {
		return false, err
	}

	return manager.db.Has([]byte(manager.blockHeightDatabaseKey), nil)
}

// GetBlockHeight returns the latest processed block height cursor.
// It returns leveldb.ErrNotFound until the first successful block is processed.
func (manager *DbManager) GetBlockHeight() (int64, error) {

	if err := manager.Validate(); err != nil {
		return 0, err
	}

	record, err := manager.db.Get([]byte(manager.blockHeightDatabaseKey), nil)
	if err != nil {
		return 0, err
	}

	height, err := decodeBlockHeight(record)
	if err != nil {
		return 0, err
	}

	if manager.logger != nil {
		manager.logger.Debug("loaded latest processed block height", "height", height)
	}

	return height, nil
}

func (manager *DbManager) getEarliestStoredRecordHeight() (int64, bool, error) {
	iter := manager.db.NewIterator(nil, nil)
	defer iter.Release()

	cursorKey := []byte(manager.blockHeightDatabaseKey)

	for iter.Next() {
		key := iter.Key()
		if len(key) != 8 {
			continue
		}
		if string(key) == string(cursorKey) {
			continue
		}

		height, err := decodeBlockHeight(key)
		if err != nil {
			return 0, false, err
		}
		return height, true, nil
	}

	if err := iter.Error(); err != nil {
		return 0, false, fmt.Errorf("iterate stored block records: %w", err)
	}

	return 0, false, nil
}

// Close closes the underlying LevelDB handle and marks the manager unusable.
func (manager *DbManager) Close() error {

	if err := manager.Validate(); err != nil {
		return err
	}

	err := manager.db.Close()
	manager.db = nil

	if err == nil && manager.logger != nil {
		manager.logger.Info("leveldb closed")
	}

	return err
}
