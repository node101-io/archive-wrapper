package indexer

import (
	archiveTypes "archive-wrapper/types"
)

// Groups actions by block height and creates a DbRecord for each block height with its corresponding actions.
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
