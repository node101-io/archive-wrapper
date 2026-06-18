package database

import (
	"archive-wrapper/errors"
	"archive-wrapper/types"

	archiveTypes "archive-wrapper/types"
	"fmt"

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
		return errors.ErrNilManager
	}

	if manager.db == nil {
		return errors.ErrUninitializedDB
	}

	return nil
}

func (manager *DbManager) Insert(record archiveTypes.DbRecord) error {

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

	return manager.db.Put(IntToBytes(record.Key), marshalled, nil)
}

func (manager *DbManager) Has(height int64) (bool, error) {

	if err := manager.Validate(); err != nil {
		return false, err
	}

	exists, err := manager.db.Has(IntToBytes(height), nil)
	if err != nil {
		return false, err
	}

	return exists, err
}

func (manager *DbManager) Get(height int64) (archiveTypes.DbRecord, error) {

	if err := manager.Validate(); err != nil {
		return archiveTypes.DbRecord{}, err
	}

	marshalled, err := manager.db.Get(IntToBytes(height), nil)
	if err != nil {
		return archiveTypes.DbRecord{}, err
	}

	var record archiveTypes.DbRecord
	err = proto.Unmarshal(marshalled, &record)
	if err != nil {
		return archiveTypes.DbRecord{}, err
	}

	return record, nil
}

func (manager *DbManager) InsertBlockHeight(height int64) error {

	if err := manager.Validate(); err != nil {
		return err
	}

	return manager.db.Put([]byte(types.BlockHeightDatabaseKey), IntToBytes(height), nil)
}

func (manager *DbManager) GetBlockHeight() (int64, error) {

	if err := manager.Validate(); err != nil {
		return 0, err
	}

	record, err := manager.db.Get([]byte(types.BlockHeightDatabaseKey), nil)
	if err != nil {
		return 0, err
	}

	return BytesToInt(record), nil
}

func (manager *DbManager) Close() error {

	if err := manager.Validate(); err != nil {
		return err
	}

	err := manager.db.Close()
	manager.db = nil

	return err
}
