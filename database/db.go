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

type DbManager struct {
	logger                 *slog.Logger
	db                     *leveldb.DB
	blockHeightDatabaseKey string
	blockHeightMu          sync.Mutex
}

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

func (manager *DbManager) HasBlockHeight() (bool, error) {
	if err := manager.Validate(); err != nil {
		return false, err
	}

	return manager.db.Has([]byte(manager.blockHeightDatabaseKey), nil)
}

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
