package vm

import (
	"fmt"
	"math/big"
	gostrings "strings"

	fjson "github.com/styrainc/load-private/pkg/json"

	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func memberBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[0]) || isUndefinedType(args[1]) {
		return nil
	}

	var found bool

	if err := func(f func(key, value interface{}) bool) error {
		return state.ValueOps().Iter(state.Globals.Ctx, args[1], *noescape(&f))
	}(func(_, v interface{}) bool {
		found, _ = state.ValueOps().Equal(state.Globals.Ctx, args[0], v)
		return found
	}); err != nil {
		return err
	}

	state.SetReturnValue(Unused, state.ValueOps().MakeBoolean(found))
	return nil
}

func memberWithKeyBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[0]) || isUndefinedType(args[1]) || isUndefinedType(args[2]) {
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
	if isUndefinedType(args[0]) || isUndefinedType(args[1]) || isUndefinedType(args[2]) {
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

	len, err := state.ValueOps().Len(state.Globals.Ctx, path)
	if err != nil {
		return err
	}
	eq, err := state.ValueOps().Equal(state.Globals.Ctx, len, state.ValueOps().MakeNumberZero())
	if err != nil {
		return err
	}
	if eq {
		state.SetReturnValue(Unused, obj)
		return nil
	}

	var found bool

	if err := func(f func(key, value interface{}) bool) error {
		return state.ValueOps().Iter(state.Globals.Ctx, path, *noescape(&f))
	}(func(_, v interface{}) bool { // path array values are our object keys
		obj, found, _ = state.ValueOps().Get(state.Globals.Ctx, obj, v)
		return !found // always iterate path array to the end if found
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

func stringsStartsWithBuiltin(state *State, args []Value) error {
	if isUndefinedType(args[0]) || isUndefinedType(args[1]) {
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
		case fjson.String:
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

func builtinStringOperand(state *State, value Value, pos int) (string, error) {
	s, ok := value.(fjson.String)
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
