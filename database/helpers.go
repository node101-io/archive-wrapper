package database

import (
	archiveTypes "archive-wrapper/types"
	"encoding/binary"
	"fmt"
)

func IntToBytes(height int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(height))
	return b[:]
}

func validateRecord(record archiveTypes.DbRecord) error {
	if record.Key < 0 {
		return fmt.Errorf("record key %d must be non-negative", record.Key)
	}

	if len(record.Actions) == 0 {
		return fmt.Errorf("record for key %d must contain at least one action", record.Key)
	}

	for i, act := range record.Actions {
		if act == nil {
			return fmt.Errorf("record action[%d] is nil", i)
		}

		if act.BlockHeight < 0 {
			return fmt.Errorf("record action[%d] has negative block height %d", i, act.BlockHeight)
		}

		if act.BlockHeight != record.Key {
			return fmt.Errorf(
				"record key %d does not match action[%d] block height %d",
				record.Key, i, act.BlockHeight,
			)
		}

		if act.Amount <= 0 {
			return fmt.Errorf("record action[%d] has invalid amount %d", i, act.Amount)
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
