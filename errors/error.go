package errors

import "errors"

var (
	ErrNilManager      = errors.New("nil manager")
	ErrUninitializedDB = errors.New("db manager is not initialized")

	ErrRecordNotExists = errors.New("record does not exists")
)
