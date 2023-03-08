package vm

import (
	"fmt"
	"math/big"
	gostrings "strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func memberBuiltin(state *State, args []Value) error {
	if isUnset(args[0]) || isUnset(args[1]) {
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

	state.SetValue(Unused, state.ValueOps().MakeBoolean(found))
	state.SetReturn(Unused)
	return nil
}

func memberWithKeyBuiltin(state *State, args []Value) error {
	if isUnset(args[0]) || isUnset(args[1]) || isUnset(args[2]) {
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

	state.SetValue(Unused, state.ValueOps().MakeBoolean(eq))
	state.SetReturn(Unused)
	return nil
}

func objectGetBuiltin(state *State, args []Value) error {
	if isUnset(args[0]) || isUnset(args[1]) || isUnset(args[2]) {
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
	eq, err := state.ValueOps().Equal(state.Globals.Ctx, len, state.ValueOps().MakeNumberInt(0))
	if err != nil {
		return err
	}
	if eq {
		state.SetValue(Unused, obj)
		state.SetReturn(Unused)
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
		state.SetValue(Unused, obj)
	} else {
		state.SetValue(Unused, def)
	}
	state.SetReturn(Unused)
	return nil
}

func objectGetBuiltinKey(state *State, obj, key, def Value) error {
	val, found, err := state.ValueOps().ObjectGet(state.Globals.Ctx, obj, key)
	if err != nil {
		return err
	}
	if found {
		state.SetValue(Unused, val)
	} else {
		state.SetValue(Unused, def)
	}
	state.SetReturn(Unused)
	return nil
}

func stringsStartsWithBuiltin(state *State, args []Value) error {
	if isUnset(args[0]) || isUnset(args[1]) {
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

	result := state.ValueOps().FromInterface(gostrings.HasPrefix(s, prefix))
	state.SetValue(Unused, result)
	state.SetReturn(Unused)
	return nil
}

func stringsSprintfBuiltin(state *State, args []Value) error {
	if isUnset(args[0]) || isUnset(args[1]) {
		return nil
	}

	s, err := builtinStringOperand(state, args[0], 1)
	if err != nil {
		return err
	}

	v, err := state.ValueOps().ToAST(state.Globals.Ctx, args[1])
	if err != nil {
		return err
	}

	astArr, ok := v.(*ast.Array)
	if !ok {
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
		switch v := astArr.Elem(i).Value.(type) {
		case ast.Number:
			if n, ok := v.Int(); ok {
				a[i] = n
			} else if b, ok := new(big.Int).SetString(v.String(), 10); ok {
				a[i] = b
			} else if f, ok := v.Float64(); ok {
				a[i] = f
			} else {
				a[i] = v.String()
			}
		case ast.String:
			a[i] = string(v)
		default:
			a[i] = astArr.Elem(i).String()
		}
	}

	state.SetValue(Unused, state.ValueOps().MakeString(fmt.Sprintf(s, a...)))
	state.SetReturn(Unused)
	return nil
}

func builtinStringOperand(state *State, value Value, pos int) (string, error) {
	v, err := state.ValueOps().ToInterface(state.Globals.Ctx, value)
	if err != nil {
		v, err := state.ValueOps().ToAST(state.Globals.Ctx, value)
		if err != nil {
			return "", err
		}

		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, &topdown.Error{
			Code:    topdown.TypeErr,
			Message: builtins.NewOperandTypeErr(pos, v, "string").Error(),
		})

		return "", err
	}

	s, ok := v.(string)
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

	return s, nil
}
