package vm

import (
	"context"

	"github.com/open-policy-agent/opa/ast"
)

type (
	DataOperations interface {
		// FromInterface converts a golang native data to internal representation.
		FromInterface(x interface{}) interface{}
		ToInterface(ctx context.Context, v interface{}) (interface{}, error)
		ToAST(ctx context.Context, v interface{}) (ast.Value, error)
		CopyShallow(value interface{}) interface{}
		Equal(ctx context.Context, a, b interface{}) (bool, error)
		IsArray(ctx context.Context, v interface{}) (bool, error)
		IsObject(ctx context.Context, v interface{}) (bool, error)
		Len(ctx context.Context, v interface{}) (interface{}, error)
		Iter(ctx context.Context, v interface{}, f func(key, value interface{}) bool) error
		Get(ctx context.Context, value, key interface{}) (interface{}, bool, error)
		GetCall(ctx context.Context, value, key interface{}) (interface{}, bool, error)
		IsCall(v interface{}) (bool, error)
		Call(ctx context.Context, value interface{}, args []*interface{}, caller *State) (interface{}, bool, bool, error)
		MakeArray(capacity int32) interface{}
		ArrayAppend(ctx context.Context, array interface{}, value interface{}) (interface{}, error)
		MakeBoolean(v bool) interface{}
		MakeObject() interface{}
		ObjectGet(ctx context.Context, object, key interface{}) (interface{}, bool, error)
		ObjectInsert(ctx context.Context, object, key, value interface{}) error
		ObjectMerge(a, b interface{}) (interface{}, error)
		MakeNull() interface{}
		MakeNumberInt(i int64) interface{}
		MakeNumberRef(n interface{}) interface{}
		MakeSet() interface{}
		SetAdd(ctx context.Context, set, value interface{}) error
		MakeString(v string) interface{}
	}
)
