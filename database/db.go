package database

import (
	"github.com/node101-io/archive-wrapper/apperrors"

	"github.com/node101-io/archive-wrapper/types"

	"fmt"

	actions "github.com/node101-io/archive-wrapper/actions"

	proto "github.com/cosmos/gogoproto/proto"
	"github.com/syndtr/goleveldb/leveldb"
)

type DbManager struct {
	db *leveldb.DB
}

func NewDbManager(path string) (*DbManager, error) {

	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, fmt.Errorf("open leveldb: %w", err)
	}

	return &DbManager{
		db: db,
	}, nil
}

func (manager *DbManager) Validate() error {

	if manager == nil {
		return apperrors.ErrNilManager
	}

	if manager.db == nil {
		return apperrors.ErrUninitializedDB
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

	return record, nil
}

func (manager *DbManager) InsertBlockHeight(height int64) error {

	if err := manager.Validate(); err != nil {
		return err
	}

	if height <= 0 {
		return apperrors.ErrBlockHeightMustBeBiggerThanZero
	}

	return manager.db.Put([]byte(types.BlockHeightDatabaseKey), encodeBlockHeight(height), nil)
}

func (manager *DbManager) HasBlockHeight() (bool, error) {
	if err := manager.Validate(); err != nil {
		return false, err
	}

	return manager.db.Has([]byte(types.BlockHeightDatabaseKey), nil)
}

func (manager *DbManager) GetBlockHeight() (int64, error) {

	if err := manager.Validate(); err != nil {
		return 0, err
	}

	record, err := manager.db.Get([]byte(types.BlockHeightDatabaseKey), nil)
	if err != nil {
		return 0, err
	}

	return decodeBlockHeight(record)
}

func (manager *DbManager) Close() error {

	if err := manager.Validate(); err != nil {
		return err
	}

	err := manager.db.Close()
	manager.db = nil

	return err
}
