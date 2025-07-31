// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package sql

import (
	"fmt"

	"github.com/open-policy-agent/opa/v1/storage"
)

const ArrayIndexTypeMsg = "array index must be integer"
const DoesNotExistMsg = "document does not exist"
const OutOfRangeMsg = "array index out of range"

func NewNotFoundError(path storage.Path) *storage.Error {
	return NewNotFoundErrorWithHint(path, DoesNotExistMsg)
}

func NewNotFoundErrorWithHint(path storage.Path, hint string) *storage.Error {
	return NewNotFoundErrorf("%v: %v", path.String(), hint)
}

func NewNotFoundErrorf(f string, a ...interface{}) *storage.Error {
	msg := fmt.Sprintf(f, a...)
	return &storage.Error{
		Code:    storage.NotFoundErr,
		Message: msg,
	}
}

func NewWriteConflictError(p storage.Path) *storage.Error {
	return &storage.Error{
		Code:    storage.WriteConflictErr,
		Message: p.String(),
	}
}

var errNotFound = &storage.Error{Code: storage.NotFoundErr}

func wrapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*storage.Error); ok {
		return err
	}

	return &storage.Error{Code: storage.InternalErr, Message: err.Error()}
}
