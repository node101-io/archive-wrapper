package indexer

import (
	"sort"

	actions "github.com/node101-io/archive-wrapper/actions"
)

// Groups actions by block height and creates a DbRecord for each block height with its corresponding actions.
func IndexActions(items []actions.Action) []actions.DbRecord {
	m := make(map[int64][]*actions.Action)

	for _, act := range items {
		m[act.BlockHeight] = append(m[act.BlockHeight], &act)
	}

	heights := make([]int64, 0, len(m))
	for height := range m {
		heights = append(heights, height)
	}

	sort.Slice(heights, func(i, j int) bool {
		return heights[i] < heights[j]
	})

	records := make([]actions.DbRecord, 0, len(m))
	for _, height := range heights {
		record := actions.DbRecord{
			Key:     height,
			Actions: m[height],
		}
		records = append(records, record)
	}

	return records
}
