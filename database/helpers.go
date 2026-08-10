package database

import (
	"bytes"
	"encoding/binary"

	cosmosErrors "cosmossdk.io/errors"
	actions "github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"
	minafield "github.com/node101-io/mina-signer-go/field"
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

		if act.BlockHeight <= 0 {
			return apperrors.ErrBlockHeightMustBeBiggerThanZero
		}

		if act.BlockHeight != record.Key {
			return apperrors.ErrInvalidKey
		}

		if act.Amount <= 0 {
			return cosmosErrors.Wrap(apperrors.ErrInvalidAmount, "non-positive amount")
		}

		if len(act.XCoordinate) == 0 {
			return apperrors.ErrEmptyXCoordinate
		}

		fieldElement, err := minafield.NewFieldElement(act.XCoordinate)
		if err != nil || !bytes.Equal(fieldElement.Bytes(), act.XCoordinate) {
			return cosmosErrors.Wrap(apperrors.ErrInvalidActionData, "invalid x coordinate")
		}
	}

	return nil
}
