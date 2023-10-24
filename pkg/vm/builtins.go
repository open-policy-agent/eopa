package vm

import (
	"context"
	"fmt"
	"math"
	"math/big"
	gostrings "strings"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"golang.org/x/exp/slices"
)

func memberBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	var found bool

	if err := func(f func(key, value interface{}) (bool, error)) error {
		return state.ValueOps().Iter(state.Globals.Ctx, args[1], *noescape(&f))
	}(func(_, v interface{}) (bool, error) {
		found, _ = state.ValueOps().Equal(state.Globals.Ctx, args[0], v)
		return found, nil
	}); err != nil {
		return err
	}

	state.SetReturnValue(Unused, state.ValueOps().MakeBoolean(found))
	return nil
}

func memberWithKeyBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[2]) || isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	var eq bool
	v, ok, err := state.ValueOps().Get(state.Globals.Ctx, args[2], args[0])
	if err != nil {
		return err
	}
	if ok {
		var err error
		eq, err = state.ValueOps().Equal(state.Globals.Ctx, args[1], v)
		if err != nil {
			return err
		}
	}

	state.SetReturnValue(Unused, state.ValueOps().MakeBoolean(eq))
	return nil
}

func objectGetBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[2]) || isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}
	obj, path, def := args[0], args[1], args[2]

	if isObj, err := state.ValueOps().IsObject(state.Globals.Ctx, obj); err != nil {
		return err
	} else if !isObj {
		x, err := state.ValueOps().ToAST(state.Globals.Ctx, obj)
		if err != nil {
			return err
		}
		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(1, x, "object").Error(),
		})
		return nil
	}

	if isPath, err := state.ValueOps().IsArray(state.Globals.Ctx, path); err != nil {
		return err
	} else if !isPath {
		return objectGetBuiltinKey(state, obj, path, def)
	}

	length, err := state.ValueOps().Len(state.Globals.Ctx, path)
	if err != nil {
		return err
	}
	eq, err := state.ValueOps().Equal(state.Globals.Ctx, length, state.ValueOps().MakeNumberZero())
	if err != nil {
		return err
	}
	if eq {
		state.SetReturnValue(Unused, obj)
		return nil
	}

	var found bool

	if err := func(f func(key, value interface{}) (bool, error)) error {
		return state.ValueOps().Iter(state.Globals.Ctx, path, *noescape(&f))
	}(func(_, v interface{}) (bool, error) { // path array values are our object keys
		obj, found, _ = state.ValueOps().Get(state.Globals.Ctx, obj, v)
		return !found, nil // always iterate path array to the end if found
	}); err != nil {
		return err
	}

	if found {
		state.SetReturnValue(Unused, obj)
	} else {
		state.SetReturnValue(Unused, def)
	}
	return nil
}

func objectKeysBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[0]) {
		return nil
	}
	obj := args[0]
	if ok, err := builtinObjectOperand(state, obj, 1); err != nil || !ok {
		return err
	}

	var set Value = state.ValueOps().MakeSet()
	switch o := obj.(type) {
	case fjson.Object:
		keys := o.Names()
		for _, k := range keys {
			if err := state.ValueOps().SetAdd(state.Globals.Ctx, set, state.ValueOps().MakeString(k)); err != nil {
				return err
			}
		}
	case IterableObject:
		err := o.Iter(state.Globals.Ctx, func(key any, _ any) (bool, error) {
			err := state.ValueOps().SetAdd(state.Globals.Ctx, set, key)
			return err != nil, err
		})
		if err != nil {
			return err
		}
	}

	state.SetReturnValue(Unused, set)
	return nil
}

func objectRemoveBuiltin(state *State, args []Value) error {
	return objectSelect(state, args, false)
}

func objectFilterBuiltin(state *State, args []Value) error {
	return objectSelect(state, args, true)
}

func objectSelect(state *State, args []Value, keep bool) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}
	obj, coll := args[0], args[1]

	if ok, err := builtinObjectOperand(state, args[0], 1); err != nil || !ok {
		return err
	}

	result := NewObject()
	var selected func(key fjson.Json) (bool, error)

	switch coll := coll.(type) {
	case IterableObject:
		selected = func(key fjson.Json) (bool, error) {
			_, ok, err := coll.Get(state.Globals.Ctx, key)
			return err == nil && ok, err
		}
	case fjson.Object:
		selected = func(key fjson.Json) (bool, error) {
			skey, ok := key.(*fjson.String)
			return ok && coll.Value(skey.Value()) != nil, nil
		}
	case *Set:
		selected = func(key fjson.Json) (bool, error) {
			_, ok, err := coll.Get(state.Globals.Ctx, key)
			return err == nil && ok, err
		}
	case fjson.Array:
		s := NewSet()
		for i := 0; i < coll.Len(); i++ {
			if err := s.Add(state.Globals.Ctx, coll.Iterate(i)); err != nil {
				return err
			}
		}
		selected = func(key fjson.Json) (bool, error) {
			_, ok, err := s.Get(state.Globals.Ctx, key)
			return err == nil && ok, err
		}
	default:
		v, err := state.ValueOps().ToAST(state.Globals.Ctx, coll)
		if err != nil {
			return err
		}

		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(2, v, "object", "set", "array").Error(),
		})
		return nil
	}

	if err := state.ValueOps().Iter(state.Globals.Ctx, obj, func(k, v any) (bool, error) {
		key := k.(fjson.Json)
		ok, err := selected(key)
		if err != nil {
			return true, err // abort
		}
		if !ok && !keep {
			result.Insert(state.Globals.Ctx, k, v)
		}
		if ok && keep {
			result.Insert(state.Globals.Ctx, k, v)
		}
		return false, nil // continue
	}); err != nil {
		return err
	}

	state.SetReturnValue(Unused, result)
	return nil
}

func objectGetBuiltinKey(state *State, obj, key, def Value) error {
	val, found, err := state.ValueOps().ObjectGet(state.Globals.Ctx, obj, key)
	if err != nil {
		return err
	}
	if found {
		state.SetReturnValue(Unused, val)
	} else {
		state.SetReturnValue(Unused, def)
	}
	return nil
}

func stringsConcatBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	join, err := builtinStringOperand(state, args[0], 1)
	if err != nil {
		return err
	}

	switch x := args[1].(type) {
	case fjson.Array:
		array := x
		n := array.Len()

		var strs []string
		if n <= 4 {
			strs = make([]string, n, 4)
		} else {
			strs = make([]string, n)
		}

		for i := 0; i < n; i++ {
			str, ok := array.Iterate(i).(*fjson.String)
			if !ok {
				v, err := state.ValueOps().ToAST(state.Globals.Ctx, array)
				if err != nil {
					return err
				}

				state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
					Code:    topdown.TypeErr,
					Message: builtins.NewOperandTypeErr(2, v, "string").Error(),
				})
				return nil
			}

			strs[i] = str.Value()
		}

		result := state.ValueOps().MakeString(gostrings.Join(strs, string(join)))
		state.SetReturnValue(Unused, result)

	case *Set:
		set := x

		var strs []string
		if n := set.Len(); n <= 4 {
			strs = make([]string, 0, 4)
		} else {
			strs = make([]string, 0, n)
		}

		var err2 error
		if set.Iter(func(vv fjson.Json) bool {
			v := *noescape(&vv) // nothing below moves the v into heap as ToAST creates a deep copy.
			str, ok := v.(*fjson.String)
			if !ok {
				var v ast.Value
				v, err2 = state.ValueOps().ToAST(state.Globals.Ctx, v)
				if err2 != nil {
					return true
				}

				state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
					Code:    topdown.TypeErr,
					Message: builtins.NewOperandTypeErr(2, v, "string").Error(),
				})
				return true
			}

			strs = append(strs, str.Value())
			return false
		}) {
			return err2
		}

		slices.Sort(strs)

		result := state.ValueOps().MakeString(gostrings.Join(strs, string(join)))
		state.SetReturnValue(Unused, result)

	default:
		v, err := state.ValueOps().ToAST(state.Globals.Ctx, args[1])
		if err != nil {
			return err
		}

		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(2, v, "set", "array").Error(),
		})
	}
	return nil
}

func stringsEndsWithBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	s, err := builtinStringOperand(state, args[0], 1)
	if err != nil {
		return err
	}

	suffix, err := builtinStringOperand(state, args[1], 2)
	if err != nil {
		return err
	}

	result := state.ValueOps().MakeBoolean(gostrings.HasSuffix(s, suffix))
	state.SetReturnValue(Unused, result)
	return nil
}

func stringsStartsWithBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	s, err := builtinStringOperand(state, args[0], 1)
	if err != nil {
		return err
	}

	prefix, err := builtinStringOperand(state, args[1], 2)
	if err != nil {
		return err
	}

	result := state.ValueOps().MakeBoolean(gostrings.HasPrefix(s, prefix))
	state.SetReturnValue(Unused, result)
	return nil
}

func stringsSprintfBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[0]) || isUndefinedType(args[1]) {
		return nil
	}

	s, err := builtinStringOperand(state, args[0], 1)
	if err != nil {
		return err
	}

	astArr, ok := args[1].(fjson.Array)
	if !ok {
		v, err := state.ValueOps().ToAST(state.Globals.Ctx, args[1])
		if err != nil {
			return err
		}

		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(2, v, "array").Error(),
		})

		return nil
	}

	// Prefer allocating a fixed size slice, to keep it in stack.

	var a []interface{}
	if n := astArr.Len(); n <= 4 {
		a = make([]interface{}, n, 4)
	} else {
		a = make([]interface{}, n)
	}

	for i := range a {
		elem := astArr.Value(i)
		switch v := elem.(type) {
		case fjson.Float:
			gn := v.Value()
			if n, err := gn.Int64(); err == nil {
				a[i] = n
			} else if b, ok := new(big.Int).SetString(gn.String(), 10); ok {
				a[i] = b
			} else if f, err := gn.Float64(); err == nil {
				a[i] = f
			} else {
				a[i] = gn.String()
			}
		case *fjson.String:
			a[i] = v.Value()
		case fjson.Array, fjson.Object, *Object, *Set:
			// TODO: Object, Set have no String() implementation at the moment, whereas fjson.Array/fjson.Object
			// String()'s produce slightly different output from their AST versions.
			c, err := state.ValueOps().ToAST(state.Globals.Ctx, elem)
			if err != nil {
				return err
			}

			a[i] = c.String()

		default:
			a[i] = v.String()
		}
	}

	state.SetReturnValue(Unused, state.ValueOps().MakeString(fmt.Sprintf(s, a...)))
	return nil
}

func countBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[0]) {
		return nil
	}

	switch a := args[0].(type) {
	case fjson.Array:
		state.SetReturnValue(Unused, state.ValueOps().MakeNumberInt(int64(a.Len())))
		return nil
	case fjson.Object:
		state.SetReturnValue(Unused, state.ValueOps().MakeNumberInt(int64(a.Len())))
		return nil
	case IterableObject:
		n, err := a.Len(state.Globals.Ctx)
		if err != nil {
			return err
		}
		state.SetReturnValue(Unused, state.ValueOps().MakeNumberInt(int64(n)))
		return nil
	case *Set:
		state.SetReturnValue(Unused, state.ValueOps().MakeNumberInt(int64(a.Len())))
		return nil
	case *fjson.String:
		state.SetReturnValue(Unused, state.ValueOps().MakeNumberInt(int64(len([]rune(*a)))))
		return nil
	}

	v, err := state.ValueOps().ToAST(state.Globals.Ctx, args[0])
	if err != nil {
		return err
	}

	state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
		Code:    topdown.TypeErr,
		Message: builtins.NewOperandTypeErr(1, v, "array", "object", "set", "string").Error(),
	})
	return nil
}

func builtinStringOperand(state *State, value Value, pos int) (string, error) {
	s, ok := value.(*fjson.String)
	if !ok {
		v, err := state.ValueOps().ToAST(state.Globals.Ctx, value)
		if err != nil {
			return "", err
		}

		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(pos, v, "string").Error(),
		})
		return "", nil
	}

	return s.Value(), nil
}

func builtinSetOperand(state *State, value Value, pos int) (*Set, error) {
	s, ok := value.(*Set)
	if !ok {
		v, err := state.ValueOps().ToAST(state.Globals.Ctx, value)
		if err != nil {
			return NewSet(), err
		}

		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(pos, v, "set").Error(),
		})
		return nil, nil
	}

	return s, nil
}

func builtinObjectOperand(state *State, value Value, pos int) (bool, error) {
	switch value.(type) {
	case fjson.Object, IterableObject:
		return true, nil
	default:
		v, err := state.ValueOps().ToAST(state.Globals.Ctx, value)
		if err != nil {
			return false, err
		}

		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(pos, v, "object").Error(),
		})
		return false, nil
	}
}

func builtinArrayOperand(state *State, value Value, pos int) (fjson.Array, error) {
	a, ok := value.(fjson.Array)
	if ok {
		return a, nil
	}
	v, err := state.ValueOps().ToAST(state.Globals.Ctx, value)
	if err != nil {
		return nil, err
	}

	state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
		Code:    topdown.TypeErr,
		Message: builtins.NewOperandTypeErr(pos, v, "array").Error(),
	})
	return nil, nil
}

func builtinNumberOperand(state *State, value Value, pos int) (*fjson.Float, error) {
	a, ok := value.(fjson.Float)
	if ok {
		return &a, nil
	}
	v, err := state.ValueOps().ToAST(state.Globals.Ctx, value)
	if err != nil {
		return nil, err
	}

	state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
		Code:    topdown.TypeErr,
		Message: builtins.NewOperandTypeErr(pos, v, "number").Error(),
	})
	return nil, nil
}

func builtinIntegerOperand(state *State, value Value, pos int) (int, bool, error) {
	f, err := builtinNumberOperand(state, value, pos)
	if err != nil || f == nil {
		return 0, false, err
	}
	i, err := f.Value().Int64()
	if err != nil {
		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandErr(pos, "must be integer number but got floating-point number").Error(),
		})
		return 0, false, nil
	}
	return int(i), true, nil
}

// builtinIntegerOperandNonStrict also accepts 10e6 as a valid integer, builtinIntegerOperand
// would NOT.
func builtinIntegerOperandNonStrict(state *State, value Value, pos int) (int, bool, error) {
	f, err := builtinNumberOperand(state, value, pos)
	if err != nil || f == nil {
		return 0, false, err
	}
	i, err := f.Value().Float64()
	if err != nil {
		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandErr(pos, "must be integer number").Error(),
		})
		return 0, false, nil
	}
	if math.Mod(i, 1.0) != 0 {
		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandErr(pos, "must be integer number but got floating-point number").Error(),
		})
		return 0, false, nil
	}
	return int(i), true, nil
}

func walkBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[0]) {
		return nil
	}

	var arr Value = state.ValueOps().MakeArray(0)

	err := do(state, state.ValueOps().MakeArray(0), args[0], func(state *State, path, val Value) error {
		tuple, _, err := state.ValueOps().ArrayAppend(state.Globals.Ctx, state.ValueOps().MakeArray(2), path)
		if err != nil {
			return err
		}
		tuple, _, err = state.ValueOps().ArrayAppend(state.Globals.Ctx, tuple, val)
		if err != nil {
			return err
		}
		arr, _, err = state.ValueOps().ArrayAppend(state.Globals.Ctx, arr, tuple)
		return err
	})
	state.SetReturnValue(Unused, arr)
	return err
}

func do(state *State, path Value, val Value, record func(*State, Value, Value) error) error {
	if err := record(state, path, val); err != nil {
		return err
	}

	if err := state.ValueOps().Iter(state.Globals.Ctx, val, func(k, v any) (bool, error) {
		err := func(state *State, k, v Value) error {
			p, err := state.ValueOps().CopyShallow(state.Globals.Ctx, path)
			if err != nil {
				return err
			}
			p, _, err = state.ValueOps().ArrayAppend(state.Globals.Ctx, p, k)
			if err != nil {
				return err
			}
			if err := record(state, p, v); err != nil {
				return err
			}
			return do(state, p, v, record) // recurse
		}(state, k, v)
		return err != nil, err
	}); err != nil {
		return err
	}

	return nil
}

func arrayConcatBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}
	a, err := builtinArrayOperand(state, args[0], 1)
	if err != nil || a == nil {
		return err
	}
	b, err := builtinArrayOperand(state, args[1], 2)
	if err != nil || b == nil {
		return err
	}

	var ret Value = state.ValueOps().MakeArray(0)
	arrays := []any{a, b}
	for i := range arrays {
		if err := state.ValueOps().Iter(state.Globals.Ctx, arrays[i], func(_, v any) (bool, error) {
			err := func(state *State, v Value) error {
				v, err := state.ValueOps().CopyShallow(state.Globals.Ctx, v)
				if err != nil {
					return err
				}
				ret, _, err = state.ValueOps().ArrayAppend(state.Globals.Ctx, ret, v)
				return err
			}(state, v)
			return err != nil, err
		}); err != nil {
			return err
		}
	}

	state.SetReturnValue(Unused, ret)
	return nil
}

func arraySliceBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[2]) || isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	start, ok, err := builtinIntegerOperand(state, args[1], 2)
	if err != nil || !ok {
		return err
	}

	stop, ok, err := builtinIntegerOperand(state, args[2], 3)
	if err != nil || !ok {
		return err
	}

	arr, err := builtinArrayOperand(state, args[0], 1)
	if err != nil || arr == nil {
		return err
	}

	if start < 0 {
		start = 0
	}
	if stop > arr.Len() {
		stop = arr.Len()
	}
	if stop < start {
		state.SetReturnValue(Unused, state.ValueOps().MakeArray(0))
		return nil
	}

	state.SetReturnValue(Unused, arr.Slice(start, stop))
	return nil
}

func equalBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}
	ret, err := equalOp(state.Globals.Ctx, args[0], args[1])
	if err == nil {
		state.SetReturnValue(Unused, state.ValueOps().MakeBoolean(ret))
	}

	return err
}

func notEqualBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	ret, err := equalOp(state.Globals.Ctx, args[0], args[1])
	if err == nil {
		state.SetReturnValue(Unused, state.ValueOps().MakeBoolean(!ret))
	}

	return err
}

func binaryOrBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	a, err := builtinSetOperand(state, args[0], 1)
	if a == nil {
		return err
	}

	b, err := builtinSetOperand(state, args[1], 2)
	if b == nil {
		return err
	}

	result := NewSet()

	a.Iter(func(x fjson.Json) bool {
		result.Add(state.Globals.Ctx, x)
		return false
	})

	b.Iter(func(x fjson.Json) bool {
		result.Add(state.Globals.Ctx, x)
		return false
	})

	state.SetReturnValue(Unused, result)
	return nil
}

func objectUnionBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}

	if ok, err := builtinObjectOperand(state, args[0], 1); !ok || err != nil {
		return err
	}

	if ok, err := builtinObjectOperand(state, args[1], 2); !ok || err != nil {
		return err
	}

	result, err := objectUnion(state.Globals.Ctx, args[0], args[1])
	if err != nil {
		return err
	}

	state.SetReturnValue(Unused, result)
	return nil
}

func objectUnion(ctx context.Context, a, b interface{}) (interface{}, error) {
	result := NewObject()

	switch a := a.(type) {
	case fjson.Object:
		var getValue func(key string) (fjson.Json, bool, error)

		switch b := b.(type) {
		case fjson.Object:
			getValue = func(key string) (fjson.Json, bool, error) {
				v2 := b.Value(key)
				return v2, v2 != nil, nil
			}

			for _, key := range b.Names() {
				if a.Value(key) == nil {
					result.Insert(ctx, fjson.NewString(key), b.Value(key))
				}
			}

		case IterableObject:
			getValue = func(key string) (fjson.Json, bool, error) {
				value, ok, err := b.Get(ctx, fjson.NewString(key))
				if !ok || err != nil {
					return nil, ok, err
				}

				return value.(fjson.Json), true, nil
			}

			if err := b.Iter(ctx, func(key, value interface{}) (bool, error) {
				if key, ok := key.(*fjson.String); !ok || a.Value(key.Value()) == nil {
					result.Insert(ctx, key, value)
				}
				return false, nil
			}); err != nil {
				return nil, err
			}

		default:
			return b, nil
		}

		for _, key := range a.Names() {
			v2, ok, err := getValue(key)
			if err != nil {
				return nil, err
			} else if !ok {
				result.Insert(ctx, fjson.NewString(key), a.Value(key))
				continue
			}

			m, err := objectUnion(ctx, a.Value(key), v2)
			if err != nil {
				return nil, err
			}

			result.Insert(ctx, fjson.NewString(key), m)
		}

	case IterableObject:
		switch b := b.(type) {
		case fjson.Object:
			for _, key := range b.Names() {
				if _, ok, err := a.Get(ctx, fjson.NewString(key)); err != nil {
					return nil, err
				} else if !ok {
					result.Insert(ctx, key, b.Value(key))
				}
			}

			err := a.Iter(ctx, func(key, value interface{}) (bool, error) {
				k, ok := key.(fjson.Json)
				if !ok {
					result.Insert(ctx, key, value)
					return false, nil
				}

				getValue := func(key fjson.Json) (fjson.Json, bool) {
					var v2 fjson.Json
					if skey, ok := key.(*fjson.String); ok {
						v2 = b.Value(skey.Value())
					}
					return v2, v2 != nil
				}

				v2, ok := getValue(k)
				if !ok {
					result.Insert(ctx, key, value)
					return false, nil
				}

				m, err := objectUnion(ctx, value, v2)
				if err != nil {
					return true, err
				}

				result.Insert(ctx, key, m)
				return false, nil
			})
			if err != nil {
				return nil, err
			}

		case IterableObject:
			if err := b.Iter(ctx, func(key, value interface{}) (bool, error) {
				if _, ok, err := a.Get(ctx, key); err != nil {
					return true, err
				} else if !ok {
					result.Insert(ctx, key, value)
				}

				return false, nil
			}); err != nil {
				return nil, err
			}

			err := a.Iter(ctx, func(key, value interface{}) (bool, error) {
				k, ok := key.(fjson.Json)
				if !ok {
					result.Insert(ctx, key, value)
					return false, nil
				}

				getValue := func(key fjson.Json) (fjson.Json, bool, error) {
					value, ok, err := b.Get(ctx, key)
					if !ok || err != nil {
						return nil, ok, err
					}

					return value.(fjson.Json), true, nil
				}

				v2, ok, err := getValue(k)
				if err != nil {
					return true, err
				} else if !ok {
					result.Insert(ctx, key, value)
					return false, nil
				}

				m, err := objectUnion(ctx, value, v2)
				if err != nil {
					return true, err
				}

				result.Insert(ctx, key, m)
				return false, nil
			})
			if err != nil {
				return nil, err
			}
		default:
			return b, nil
		}

	default:
		return b, nil
	}

	return result, nil
}

func typeArray(v Value) bool {
	_, ok := v.(fjson.Array)
	return ok
}

func typeString(v Value) bool {
	_, ok := v.(*fjson.String)
	return ok
}

func typeBoolean(v Value) bool {
	_, ok := v.(fjson.Bool)
	return ok
}

func typeObject(v Value) bool {
	switch v.(type) {
	case fjson.Object, IterableObject:
		return true
	}
	return false
}

func typeSet(v Value) bool {
	_, ok := v.(*Set)
	return ok
}

func typeNumber(v Value) bool {
	_, ok := v.(fjson.Float)
	return ok
}

func typeNull(v Value) bool {
	_, ok := v.(fjson.Null)
	return ok
}

func isTypeBuiltin(state *State, args []Value, chk func(Value) bool) error {
	if isUndefinedType(args[0]) {
		return nil
	}
	state.SetReturnValue(Unused, state.ValueOps().MakeBoolean(chk(args[0])))
	return nil
}

func typename(v Value) string {
	switch v.(type) {
	case fjson.Array:
		return "array"
	case *fjson.String:
		return "string"
	case fjson.Bool:
		return "boolean"
	case fjson.Null:
		return "null"
	case fjson.Float:
		return "number"
	case fjson.Object, IterableObject:
		return "object"
	case *Set:
		return "set"
	}
	panic("unreachable")
}

func typenameBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[0]) {
		return nil
	}
	state.SetReturnValue(Unused, state.ValueOps().MakeString(typename(args[0])))
	return nil
}

func numbersRangeBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}
	start, ok, err := builtinIntegerOperandNonStrict(state, args[0], 1)
	if err != nil || !ok {
		return err
	}
	stop, ok, err := builtinIntegerOperandNonStrict(state, args[1], 2)
	if err != nil || !ok {
		return err
	}

	return numbersRange(state, start, stop, 1)
}

func numbersRangeStepBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[2]) || isUndefinedType(args[1]) || isUndefinedType(args[0]) {
		return nil
	}
	start, ok, err := builtinIntegerOperandNonStrict(state, args[0], 1)
	if err != nil || !ok {
		return err
	}
	stop, ok, err := builtinIntegerOperandNonStrict(state, args[1], 2)
	if err != nil || !ok {
		return err
	}
	step, ok, err := builtinIntegerOperandNonStrict(state, args[2], 3)
	if err != nil || !ok {
		return err
	}
	if step <= 0 {
		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.BuiltinErr,
			Message: builtins.NewOperandErr(3, "step must be a positive number above zero").Error(),
		})
		return nil
	}
	return numbersRange(state, start, stop, step)
}

func numbersRange(state *State, start, stop, step int) error {
	// NOTE(sr): This is intentionally *not* preallocated: For large lengths, make([]fjson.File, length)
	// can take more than two seconds, and we cannot check if the evaluation has been canceled during
	// that period.
	var elems []fjson.File
	var length int
	var next func(i int) int
	if start <= stop {
		length = (stop-start)/step + 1
		next = func(i int) int { return start + i*step }
	} else {
		length = (start-stop)/step + 1
		next = func(i int) int { return start - i*step }
	}
	for i := 0; i < length && !state.Globals.cancel.Cancelled(); i++ {
		elems = append(elems, fjson.NewFloatInt(int64(next(i))))
	}
	if state.Globals.cancel.Cancelled() {
		return topdown.Halt{
			Err: &topdown.Error{
				Code:    topdown.CancelErr,
				Message: "numbers.range: timed out before generating all numbers in range",
			},
		}
	}
	state.SetReturnValue(Unused, fjson.NewArray(elems, len(elems)))
	return nil
}
