// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"reflect"
	"sync"
)

type Field struct {
	Name      string
	Index     []int
	OmitEmpty bool
}

var fieldCache2 sync.Map

func CachedTypeFields(t reflect.Type) []Field {
	if f, ok := fieldCache2.Load(t); ok {
		return f.([]Field)
	}

	tf := typeFields(t)
	resolved := make([]Field, 0, len(tf.list))
	for _, f := range tf.list {
		resolved = append(resolved, Field{Name: f.name, Index: f.index, OmitEmpty: f.omitEmpty})
	}

	f, _ := fieldCache2.LoadOrStore(t, resolved)
	return f.([]Field)
}
