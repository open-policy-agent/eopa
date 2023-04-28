package storage

import (
	"errors"
	"fmt"
)

// Error is the error type returned by the storage layer.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const readsNotSupportedErr = "storage_reads_not_supported_error"

// ErrReadsNotSupported indicates the caller attempted to perform a read
// against a store at a location that does not support them.
var ErrReadsNotSupported = &Error{Code: readsNotSupportedErr}

func (err *Error) Error() string {
	if err.Message != "" {
		return fmt.Sprintf("%v: %v", err.Code, err.Message)
	}
	return err.Code
}

func (err *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return t.Code == err.Code && (t.Message == "" || t.Message == err.Message)
	}
	return false
}
