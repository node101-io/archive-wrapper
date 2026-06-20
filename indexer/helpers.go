package indexer

import (
	"sort"

	archiveTypes "github.com/node101-io/archive-wrapper/types"
)

// Groups actions by block height and creates a DbRecord for each block height with its corresponding actions.
func IndexActions(actions []archiveTypes.Action) []archiveTypes.DbRecord {
	m := make(map[int64][]*archiveTypes.Action)

	for _, act := range actions {
		m[act.BlockHeight] = append(m[act.BlockHeight], &act)
	}

	heights := make([]int64, 0, len(m))
	for height := range m {
		heights = append(heights, height)
	}

	sort.Slice(heights, func(i, j int) bool {
		return heights[i] < heights[j]
	})

	records := make([]archiveTypes.DbRecord, 0, len(m))
	for _, height := range heights {
		record := archiveTypes.DbRecord{
			Key:     height,
			Actions: m[height],
		}
		records = append(records, record)
	}

	return records
}
