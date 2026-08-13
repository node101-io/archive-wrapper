package database

import (
	"encoding/binary"

	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
)

func encodeBlockHeight(height int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(height))
	return b[:]
}

func decodeBlockHeight(b []byte) (int64, error) {

	if len(b) != 8 {
		return 0, apperrors.ErrInvalidLength
	}

	return int64(binary.BigEndian.Uint64(b)), nil
}

func validateRecord(record actions.DbRecord) error {

	if record.Key <= 0 {
		return apperrors.ErrBlockHeightMustBeBiggerThanZero
	}

	for _, act := range record.Actions {
		if act == nil {
			return apperrors.ErrNilAction
		}

		if act.BlockHeight != record.Key {
			return apperrors.ErrInvalidKey
		}

	}

	return nil
}
