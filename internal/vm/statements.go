package vm

import (
	gojson "encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func (p plan) Execute(state *State) error {
	var err error
	blocks := p.Blocks()

	for i := 0; i < blocks.Len() && err == nil; i++ {
		_, _, err = blocks.Block(i).Execute(state)
	}

	return err
}

func (f function) Execute(state *State, args []*Value) error {
	if !f.IsBuiltin() {
		return f.execute(state, args)
	}

	return builtin(f).Execute(state, args)
}

func (f function) execute(state *State, args []*Value) error {
	index, params, ret := f.Index(), f.Params(), f.Return()

	memoize := len(params) == 2
	if memoize {
		if value, ok := state.MemoizeGet(index); ok {
			if value != nil {
				state.SetValue(ret, *value)
				state.SetReturn(ret)
			}
			// else {
			// undefined return
			// }

			return nil
		}
	}

	for i := range args {
		if args[i] != nil {
			state.SetValue(params[i], *args[i])
		} else {
			state.Unset(params[i])
		}
	}

	err := state.Instr()
	blocks := f.Blocks()

	for i := 0; i < blocks.Len() && err == nil; i++ {
		_, _, err = blocks.Block(i).Execute(state)

		// TODO: No need to wrap the statements of the last block.
	}

	if memoize {
		var value *Value
		if local, defined := state.Return(); defined {
			v := state.Value(local)
			value = &v
		}

		state.MemoizeInsert(index, value)
	}

	return err
}

func (builtin builtin) Execute(state *State, args []*Value) error {
	if err := state.Instr(); err != nil {
		return err
	}

	bctx := topdown.BuiltinContext{
		Context: state.Globals.Ctx,
		Metrics: state.Globals.Metrics,
		Seed:    state.Globals.Seed,
		Time:    ast.NumberTerm(gojson.Number(strconv.FormatInt(state.Globals.Time.UnixNano(), 10))),
		// Cancel:                 e.cancel,
		// Runtime:                e.runtime,
		Cache: builtins.Cache{}, // TODO
		// InterQueryBuiltinCache: e.interQueryBuiltinCache,
		// Location:               e.query[e.index].Location,
		// QueryTracers:           e.tracers,
		// TraceEnabled:           e.traceEnabled,
		// QueryID:                e.queryID,
		// ParentID:               parentID,
		PrintHook: state.Globals.PrintHook,
	}

	a := make([]*ast.Term, len(args))
	for i := range args {
		if args[i] == nil {
			return nil
		}

		v, err := state.ValueOps().ToAST(state.Globals.Ctx, *args[i])
		if err != nil {
			return err
		}

		a[i] = ast.NewTerm(v)
	}

	relation := builtin.Relation()
	if relation {
		state.SetValue(Unused, state.ValueOps().MakeArray(0))
		state.SetReturn(Unused)
	}

	impl := topdown.GetBuiltin(builtin.Name())
	if impl == nil {
		return fmt.Errorf("builtin not found: %s", builtin.Name())
	}

	if err := impl(bctx, a, func(value *ast.Term) error {
		if relation {
			arr, err := state.ValueOps().ArrayAppend(state.Globals.Ctx, state.Value(Unused), state.ValueOps().FromInterface(value.Value))
			if err != nil {
				return err
			}

			state.SetValue(Unused, arr)
		} else {
			state.SetValue(Unused, state.ValueOps().FromInterface(value.Value))
			state.SetReturn(Unused)
		}
		return nil
	}); err != nil {
		var t topdown.Halt
		if errors.As(err, &t) {
			return err
		} else {
			state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, err)
		}
	}

	return nil
}

func (b block) Execute(state *State) (bool, uint32, error) {
	var stop bool
	var index uint32
	err := state.Instr()

	statements := b.Statements()
	for i := 0; !stop && err == nil && i < statements.Len(); i++ {
		// fmt.Printf("executing %d/%d: %T\n", i, len(b.Statements), b.Statements[i])
		stop, index, err = statements.Statement(i).Execute(state)
	}

	if err != nil {
		return false, 0, err
	}

	if stop {
		if index == 0 {
			// Execution continues after this block.
			return false, 0, nil
		}

		// More blocks to jump after this.
		return true, index - 1, err
	}

	return false, 0, nil
}

func (s statement) Execute(state *State) (bool, uint32, error) {
	switch s.Type() {
	case typeStatementArrayAppend:
		return arrayAppend(s).Execute(state)
	case typeStatementAssignInt:
		return assignInt(s).Execute(state)
	case typeStatementAssignVar:
		return assignVar(s).Execute(state)
	case typeStatementAssignVarOnce:
		return assignVarOnce(s).Execute(state)
	case typeStatementBlockStmt:
		return blockStmt(s).Execute(state)
	case typeStatementBreakStmt:
		return breakStmt(s).Execute(state)
	case typeStatementCall:
		return call(s).Execute(state)
	case typeStatementCallDynamic:
		return callDynamic(s).Execute(state)
	case typeStatementDot:
		return dot(s).Execute(state)
	case typeStatementEqual:
		return equal(s).Execute(state)
	case typeStatementIsArray:
		return isArray(s).Execute(state)
	case typeStatementIsDefined:
		return isDefined(s).Execute(state)
	case typeStatementIsObject:
		return isObject(s).Execute(state)
	case typeStatementIsUndefined:
		return isUndefined(s).Execute(state)
	case typeStatementLen:
		return lenStmt(s).Execute(state)
	case typeStatementMakeArray:
		return makeArray(s).Execute(state)
	case typeStatementMakeNull:
		return makeNull(s).Execute(state)
	case typeStatementMakeNumberInt:
		return makeNumberInt(s).Execute(state)
	case typeStatementMakeNumberRef:
		return makeNumberRef(s).Execute(state)
	case typeStatementMakeObject:
		return makeObject(s).Execute(state)
	case typeStatementMakeSet:
		return makeSet(s).Execute(state)
	case typeStatementNop:
		return nop(s).Execute(state)
	case typeStatementNot:
		return not(s).Execute(state)
	case typeStatementNotEqual:
		return notEqual(s).Execute(state)
	case typeStatementObjectInsert:
		return objectInsert(s).Execute(state)
	case typeStatementObjectInsertOnce:
		return objectInsertOnce(s).Execute(state)
	case typeStatementObjectMerge:
		return objectMerge(s).Execute(state)
	case typeStatementResetLocal:
		return resetLocal(s).Execute(state)
	case typeStatementResultSetAdd:
		return resultSetAdd(s).Execute(state)
	case typeStatementReturnLocal:
		return returnLocal(s).Execute(state)
	case typeStatementScan:
		return scan(s).Execute(state)
	case typeStatementSetAdd:
		return setAdd(s).Execute(state)
	case typeStatementWith:
		return with(s).Execute(state)
	default:
		panic(fmt.Sprintf("unsupported statement: %v", s.Type()))
	}
}

func (nop) Execute(state *State) (bool, uint32, error) {
	return false, 0, state.Instr()
}

func (a assignInt) Execute(state *State) (bool, uint32, error) {
	state.SetValue(a.Target(), state.ValueOps().MakeNumberInt(a.Value()))
	return false, 0, state.Instr()
}

func (a assignVarOnce) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	target, source := a.Target(), a.Source()

	if defined := state.IsDefined(target); defined {
		if !state.IsDefined(source) {
			return false, 0, ErrVarAssignConflict
		}

		if eq, err := state.ValueOps().Equal(state.Globals.Ctx, state.Value(source), state.Value(target)); err != nil {
			return false, 0, err
		} else if !eq {
			return false, 0, ErrVarAssignConflict
		}

		return false, 0, nil
	}

	state.Set(target, source)
	return false, 0, nil
}

func (a assignVar) Execute(state *State) (bool, uint32, error) {
	state.Set(a.Target(), a.Source())
	return false, 0, state.Instr()
}

func (s scan) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	var stop bool
	var n uint32
	var err error

	// TODO: Should break index=1 if the source is not iterable?

	source, skey, svalue := s.Source(), s.Key(), s.Value()

	err2 := state.ValueOps().Iter(state.Globals.Ctx, state.Value(source), func(key, value interface{}) bool {
		state.SetValue(skey, key)
		state.SetValue(svalue, value)

		stop, n, err = s.Block().Execute(state)
		if stop || err != nil {
			return true
		}

		return false
	})
	if err2 != nil {
		return false, 0, err2
	} else if err != nil {
		return false, 0, err
	}

	if stop && n > 0 {
		// Further blocks to skip.
		return true, n - 1, nil
	}

	return false, 0, nil
}

func (b blockStmt) Execute(state *State) (bool, uint32, error) {
	var stop bool
	var n uint32
	err := state.Instr()

	blocks := b.Blocks()
	for i := 0; i < blocks.Len() && err == nil && !stop; i++ {
		stop, n, err = blocks.Block(i).Execute(state)
	}

	// Block statement is not considered a nested block and hence
	// do not decrement the index.

	return stop, n, err
}

func (b breakStmt) Execute(state *State) (bool, uint32, error) {
	return true, b.Index(), state.Instr()
}

func (n not) Execute(state *State) (bool, uint32, error) {
	var stop bool
	var index uint32
	err := state.Instr()

	statements := n.Block().Statements()

	for i := 0; !stop && err == nil && i < statements.Len(); i++ {
		stop, index, err = statements.Statement(i).Execute(state)
	}

	if err != nil {
		return false, 0, err
	}

	if stop {
		if index == 0 {
			return false, 0, nil
		}

		return true, index - 1, err
	}

	return true, 0, nil
}

func (r returnLocal) Execute(state *State) (bool, uint32, error) {
	state.SetReturn(r.Source())
	return false, 0, state.Instr()
}

func (call callDynamic) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	inner := state.New()

	cargs, cpath := call.Args(), call.Path()

	args := make([]*Value, len(cargs))
	for i := range cargs {
		if state.IsDefined(cargs[i]) {
			v := state.Value(cargs[i])
			args[i] = &v
		}
	}

	var path []string
	for i := range cpath {
		if !state.IsDefined(cpath[i]) {
			panic("undefined call dynamic path")
		}

		s, err := state.ValueOps().ToAST(state.Globals.Ctx, state.Value(cpath[i]))
		if err != nil {
			return false, 0, err
		}

		path = append(path, string(s.(ast.String)))
	}

	f, _ := state.FindByPath(path)
	if f == nil {
		value, ok, mapping, err := externalCall(state, path, args)
		if err != nil {
			return false, 0, err
		} else if !mapping {
			// mapping not found
			return true, 0, nil
		} else if !ok {
			// mapping found, "undefined" result counts
			return true, 3, nil
		}

		state.SetValue(call.Result(), value)
		return false, 0, nil
	}

	if err := f.Execute(inner, args); err != nil {
		return false, 0, err
	}

	result, ok := inner.Return()
	if !ok {
		// mapping found, "undefined" result counts
		return true, 3, nil
	}

	state.SetFrom(call.Result(), inner, result)
	return false, 0, nil
}

func externalCall(state *State, path []string, args []*Value) (interface{}, bool, bool, error) {
	if len(path) > 0 {
		path = path[1:]
	}

	if !state.IsDefined(Data) {
		return nil, false, false, nil
	}

	data := state.Value(Data)
	a := make([]*interface{}, len(args))
	for i := range a {
		if args[i] != nil {
			a[i] = (*interface{})(args[i])
		}
	}

	for _, seg := range path {
		value, defined, err := state.ValueOps().GetCall(state.Globals.Ctx, data, state.ValueOps().FromInterface(seg))
		if err != nil || !defined {
			return nil, false, false, err
		}

		data = value
	}

	if ok, err := state.ValueOps().IsCall(data); err != nil {
		return nil, false, false, err
	} else if !ok {
		// Turn a call to data into plain data access, if not
		// a function call.
		if len(args) == 2 {
			return data, true, true, nil
		}

		// Function invocation into a data that was supposed
		// to be code. Data was illegally inserted.
		return nil, false, false, ErrFunctionCallToData
	}

	return state.ValueOps().Call(state.Globals.Ctx, data, a, state)
}

func (call call) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	cargs := call.Args()

	args := make([]*Value, len(cargs))
	for i := range cargs {
		if state.IsDefined(cargs[i]) {
			v := state.Value(cargs[i])
			args[i] = &v
		}
	}

	inner := state.New()

	if err := state.Func(call.Func()).Execute(inner, args); err != nil {
		return false, 0, err
	}

	result, ok := inner.Return()
	if !ok {
		return true, 0, nil
	}

	state.SetFrom(call.Result(), inner, result)
	return false, 0, nil
}

func (d dot) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	source, key, target := d.Source(), d.Key(), d.Target()

	if !state.IsDefined(source) || !state.IsDefined(key) {
		state.Unset(target)
		return true, 0, nil
	}

	src := state.Value(source) // TODO: To optimize, can skip booleans and string values directly.

	if value, ok, err := state.ValueOps().Get(state.Globals.Ctx, src, state.Value(key)); err != nil {
		return false, 0, err
	} else if ok {
		state.SetValue(target, value)
		return false, 0, nil
	}

	state.Unset(target)
	return true, 0, nil
}

func (e equal) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	a, b := e.A(), e.B()
	definedA, definedB := state.IsDefined(a), state.IsDefined(b)

	switch {
	case !definedA && !definedB:
		return false, 0, nil

	case definedA && definedB:
		eq, err := state.ValueOps().Equal(state.Globals.Ctx, state.Value(a), state.Value(b))
		if err != nil {
			return false, 0, err
		}

		return !eq, 0, nil

	default:
		return true, 0, nil
	}
}

func (ne notEqual) Execute(state *State) (bool, uint32, error) {
	stop, index, err := equal(ne).Execute(state)
	return !stop, index, err
}

func (i isArray) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	source := i.Source()
	if defined := state.IsDefined(source); !defined {
		return true, 0, nil
	}

	is, err := state.ValueOps().IsArray(state.Globals.Ctx, state.Value(source))
	if err != nil {
		return false, 0, err
	}
	return !is, 0, nil
}

func (i isObject) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	source := i.Source()
	if defined := state.IsDefined(source); !defined {
		return true, 0, nil
	}

	is, err := state.ValueOps().IsObject(state.Globals.Ctx, state.Value(source))
	if err != nil {
		return false, 0, err
	}

	return !is, 0, nil
}

func (i isDefined) Execute(state *State) (bool, uint32, error) {
	return !state.IsDefined(i.Source()), 0, state.Instr()
}

func (i isUndefined) Execute(state *State) (bool, uint32, error) {
	return state.IsDefined(i.Source()), 0, state.Instr()
}

func (m makeNull) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeNull())
	return false, 0, state.Instr()
}

func (m makeNumberInt) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeNumberInt(m.Value()))
	return false, 0, state.Instr()
}

func (m makeNumberRef) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeNumberRef(state.Value(StringIndexConst(m.Index()))))
	return false, 0, state.Instr()
}

func (m makeArray) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeArray(m.Capacity()))
	return false, 0, state.Instr()
}

func (m makeSet) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeSet())
	return false, 0, state.Instr()
}

func (m makeObject) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeObject())
	return false, 0, state.Instr()
}

func (l lenStmt) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	n, err := state.ValueOps().Len(state.Globals.Ctx, state.Value(l.Source()))
	if err == nil {
		state.SetValue(l.Target(), n)
	}

	return false, 0, err
}

func (a arrayAppend) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	array, value := a.Array(), a.Value()
	arr, err := state.ValueOps().ArrayAppend(state.Globals.Ctx, state.Value(array), state.Value(value))
	if err != nil {
		return false, 0, err
	}

	state.SetValue(array, arr)
	return false, 0, nil
}

func (s setAdd) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	err := state.ValueOps().SetAdd(state.Globals.Ctx, state.Value(s.Set()), state.Value(s.Value()))
	return false, 0, err
}

func (o objectInsertOnce) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	ops := state.ValueOps()

	key, value, object := state.Value(o.Key()), state.Value(o.Value()), state.Value(o.Object())
	existing, ok, err := ops.ObjectGet(state.Globals.Ctx, object, key)
	if err != nil {
		return false, 0, err
	} else if ok {
		if eq, err := ops.Equal(state.Globals.Ctx, value, existing); err != nil {
			return false, 0, err
		} else if !eq {
			return false, 0, ErrObjectInsertConflict
		}
	}

	err = ops.ObjectInsert(state.Globals.Ctx, object, key, value)
	return false, 0, err
}

func (o objectInsert) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	key, value, object := state.Value(o.Key()), state.Value(o.Value()), state.Value(o.Object())
	err := state.ValueOps().ObjectInsert(state.Globals.Ctx, object, key, value)
	return false, 0, err
}

func (o objectMerge) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	ca, cb, target := o.A(), o.B(), o.Target()

	if !state.IsDefined(ca) {
		state.Set(target, cb)
		return false, 0, nil
	}

	if !state.IsDefined(cb) {
		state.Set(target, ca)
		return false, 0, nil
	}

	a, b := state.Value(ca), state.Value(cb)
	ops := state.ValueOps()

	if isObject, err := ops.IsObject(state.Globals.Ctx, a); err != nil {
		return false, 0, err
	} else if !isObject {
		return false, 0, ErrObjectInsertConflict
	}

	if isObject, err := ops.IsObject(state.Globals.Ctx, b); err != nil {
		return false, 0, err
	} else if !isObject {
		return false, 0, ErrObjectInsertConflict
	}

	m, err := ops.ObjectMerge(a, b)
	if err != nil {
		return false, 0, err
	}

	state.SetValue(target, m)
	return false, 0, nil
}

func (r resetLocal) Execute(state *State) (bool, uint32, error) {
	state.Unset(r.Target())
	return false, 0, state.Instr()
}

func (r resultSetAdd) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	value := r.Value()
	if !state.IsDefined(value) {
		return false, 0, nil
	}

	err := state.ValueOps().SetAdd(state.Globals.Ctx, state.Globals.ResultSet, state.Value(value))
	return false, 0, err
}

func (with with) Execute(state *State) (bool, uint32, error) {
	if err := state.Instr(); err != nil {
		return false, 0, err
	}

	state.MemoizePush()
	defer state.MemoizePop()

	local, path, wvalue := with.Local(), with.Path(), with.Value()

	defined, value := state.IsDefined(local), state.Value(local)
	defer func() {
		if defined {
			state.SetValue(local, value)
		} else {
			state.Unset(local)
		}
	}()

	if len(path) == 0 {
		state.Set(local, wvalue)
	} else {

		value, err := with.upsert(state, local, path, wvalue)
		if err != nil {
			return false, 0, err
		}

		state.SetValue(local, value)
	}

	statements := with.Block().Statements()
	for i := 0; i < statements.Len(); i++ {
		if stop, _, err := statements.Statement(i).Execute(state); err != nil {
			return false, 0, err
		} else if stop {
			return stop, 0, nil
		}
	}

	return false, 0, nil
}

func (w with) upsert(state *State, original Local, path []int, value LocalOrConst) (Value, error) {
	ops := state.ValueOps()

	var ok bool
	if state.IsDefined(original) {
		var err error
		ok, err = ops.IsObject(state.Globals.Ctx, state.Value(original))
		if err != nil {
			return nil, err
		}
	}

	var result Value
	if ok {
		result = ops.CopyShallow(state.Value(original))
	} else {
		result = ops.MakeObject()
	}

	nested := result
	for i := 0; i < len(path)-1; i++ {
		key := state.Value(StringIndexConst(path[i]))
		next, ok, err := ops.Get(state.Globals.Ctx, nested, key)
		if err != nil {
			return nil, err
		}

		var isObject bool
		if !ok {
			next = ops.MakeObject()
			err = ops.ObjectInsert(state.Globals.Ctx, nested, key, next)
		} else if isObject, err = ops.IsObject(state.Globals.Ctx, next); err != nil {
			// Nothing
		} else if !isObject {
			next = ops.MakeObject()
			err = ops.ObjectInsert(state.Globals.Ctx, nested, key, next)
		} else {
			next = ops.CopyShallow(next)
			err = ops.ObjectInsert(state.Globals.Ctx, nested, key, next)
		}

		if err != nil {
			return nil, err
		}

		nested = next
	}

	err := ops.ObjectInsert(state.Globals.Ctx, nested, state.Value(StringIndexConst(path[len(path)-1])), state.Value(value))
	return result, err
}
