package database

import (
	"archive-wrapper/errors"
	archiveTypes "archive-wrapper/types"
	"encoding/binary"
)

func IntToBytes(height int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(height))
	return b[:]
}

func validateRecord(record archiveTypes.DbRecord) error {

	if record.Key < 0 {
		return errors.ErrBlockHeightMustBeBiggerThanZero
	}

	for _, act := range record.Actions {
		if act == nil {
			return errors.ErrNilAction
		}

		if act.BlockHeight < 0 {
			return errors.ErrBlockHeightMustBeBiggerThanZero
		}

		if act.BlockHeight != record.Key {
			return errors.ErrInvalidKey
		}

		if act.Amount <= 0 {
			return errors.ErrAmountMustBeBiggerThanZero
		}
	}

	return nil
}

func IndexActions(actions []archiveTypes.Action) []archiveTypes.DbRecord {

	m := make(map[int64][]*archiveTypes.Action)

	for _, act := range actions {
		m[act.BlockHeight] = append(m[act.BlockHeight], &act)
	}

	var records []archiveTypes.DbRecord
	for height, actions := range m {
		record := archiveTypes.DbRecord{
			Key:     int64(height),
			Actions: actions,
		}
		records = append(records, record)
	}

	return records
}
