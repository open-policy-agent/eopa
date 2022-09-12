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

	var resolved []Field
	for _, f := range typeFields(t).list {
		resolved = append(resolved, Field{Name: f.name, Index: f.index, OmitEmpty: f.omitEmpty})
	}

	f, _ := fieldCache2.LoadOrStore(t, resolved)
	return f.([]Field)
}
