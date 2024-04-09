package vm

import (
	"errors"
	"unsafe"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

func isUndefinedType(v Value) bool {
	return v == nil
}

func undefined() Value {
	return nil
}

func (p plan) Execute(state *State) error {
	var err error
	blocks := p.Blocks()

	for i, n := 0, blocks.Len(); i < n && err == nil; i++ {
		if err = state.Instr(1); err == nil {
			_, _, err = blocks.Block(i).Execute(state)
		}
	}

	return err
}

func (f function) Execute(state *State, args []Value) error {
	if !f.IsBuiltin() {
		return f.execute(state, args)
	}

	return builtin(f).Execute(state, args)
}

func isHashable(as []Value) bool {
	if len(as) > 9 {
		return false
	}
	for i := range as {
		switch as[i].(type) {
		case IterableObject, fjson.Array, fjson.Set, fjson.Object, fjson.Object2:
			return false
		}
	}
	return true
}

func (f function) execute(state *State, args []Value) error {
	index, ret := f.Index(), f.Return()

	funArgs := args[2:f.ParamsLen()]
	memoize := isHashable(funArgs)
	if memoize {
		value, ok := state.MemoizeGet(index, funArgs)
		if ok {
			state.stats.VirtualCacheHits++

			if !isUndefinedType(value) {
				state.SetReturnValue(ret, value)
			}
			// else {
			// undefined return
			// }

			return nil
		}
		state.stats.VirtualCacheMisses++
	} else {
		state.stats.VirtualCacheMisses++
	}

	f.ParamsIter(func(i uint32, arg Local) error {
		state.SetValue(arg, args[i])
		return nil
	})

	var err error
	blocks := f.Blocks()

	for i, n := 0, blocks.Len(); i < n && err == nil; i++ {
		_, _, err = blocks.Block(i).Execute(state)

		// TODO: No need to wrap the statements of the last block.
	}

	if memoize {
		var value Value
		if local, defined := state.Return(); defined {
			value = state.Local(local)
		} else {
			value = undefined()
		}

		state.MemoizeInsert(index, funArgs, value)
	}

	return err
}

func (builtin builtin) Execute(state *State, args []Value) error {
	// Try to use a builtin implementation operating directly with
	// the internal data types. The conversions to AST data type
	// is an expensive (heap heavy) operation.

	name := builtin.Name()

	switch name {
	case ast.Member.Name:
		return memberBuiltin(state, args)
	case ast.MemberWithKey.Name:
		return memberWithKeyBuiltin(state, args)
	case ast.ObjectGet.Name:
		return objectGetBuiltin(state, args)
	case ast.ObjectKeys.Name:
		return objectKeysBuiltin(state, args)
	case ast.ObjectRemove.Name:
		return objectRemoveBuiltin(state, args)
	case ast.ObjectFilter.Name:
		return objectFilterBuiltin(state, args)
	case ast.ObjectUnion.Name:
		return objectUnionBuiltin(state, args)
	case ast.Concat.Name:
		return stringsConcatBuiltin(state, args)
	case ast.EndsWith.Name:
		return stringsEndsWithBuiltin(state, args)
	case ast.StartsWith.Name:
		return stringsStartsWithBuiltin(state, args)
	case ast.Sprintf.Name:
		return stringsSprintfBuiltin(state, args)
	case ast.ArrayConcat.Name:
		return arrayConcatBuiltin(state, args)
	case ast.ArraySlice.Name:
		return arraySliceBuiltin(state, args)
	case ast.Count.Name:
		return countBuiltin(state, args)
	case ast.WalkBuiltin.Name:
		return walkBuiltin(state, args)
	case ast.Equal.Name:
		return equalBuiltin(state, args)
	case ast.NotEqual.Name:
		return notEqualBuiltin(state, args)
	case ast.Or.Name:
		return binaryOrBuiltin(state, args)
	case ast.IsArray.Name:
		return isTypeBuiltin(state, args, typeArray)
	case ast.IsString.Name:
		return isTypeBuiltin(state, args, typeString)
	case ast.IsBoolean.Name:
		return isTypeBuiltin(state, args, typeBoolean)
	case ast.IsObject.Name:
		return isTypeBuiltin(state, args, typeObject)
	case ast.IsSet.Name:
		return isTypeBuiltin(state, args, typeSet)
	case ast.IsNumber.Name:
		return isTypeBuiltin(state, args, typeNumber)
	case ast.IsNull.Name:
		return isTypeBuiltin(state, args, typeNull)
	case ast.JSONUnmarshal.Name:
		return jsonUnmarshalBuiltin(state, args)
	case ast.TypeNameBuiltin.Name:
		return typenameBuiltin(state, args)
	case ast.NumbersRange.Name:
		return numbersRangeBuiltin(state, args)
	case ast.NumbersRangeStep.Name:
		return numbersRangeStepBuiltin(state, args)
	case ast.GlobMatch.Name:
		return globMatchBuiltin(state, args)
	}

	// If none available, revert to standard OPA builtin
	// implementations using AST types.

	bctx := topdown.BuiltinContext{
		Context:                state.Globals.Ctx,
		Metrics:                state.Globals.Metrics,
		Seed:                   state.Globals.Seed,
		Time:                   ast.UIntNumberTerm(uint64(state.Globals.Time.UnixNano())),
		Cancel:                 &state.Globals.cancel,
		Runtime:                state.Globals.Runtime,
		Cache:                  state.Globals.Cache,
		NDBuiltinCache:         state.Globals.NDBCache,
		InterQueryBuiltinCache: state.Globals.InterQueryBuiltinCache,
		PrintHook:              state.Globals.PrintHook,
		Capabilities:           state.Globals.Capabilities,
		DistributedTracingOpts: state.Globals.TracingOpts,
	}

	a := make([]*ast.Term, len(args))

	for i := range args {
		if isUndefinedType(args[i]) {
			return nil
		}

		v, err := state.ValueOps().ToAST(state.Globals.Ctx, args[i])
		if err != nil {
			return err
		}

		a[i] = ast.NewTerm(v)
	}

	if name == ast.InternalPrint.Name {
		bctx.Location = &ast.Location{}
	}

	relation := builtin.Relation()
	if relation {
		state.SetReturnValue(Unused, state.ValueOps().MakeArray(0))
	}

	var bi *ast.Builtin
	var impl topdown.BuiltinFunc

	tbi, ok := state.Globals.BuiltinFuncs[name]
	if ok {
		impl = tbi.Func
		bi = tbi.Decl
	} else {
		impl = topdown.GetBuiltin(name)
		if impl == nil {
			return errors.New("builtin not found: " + name)
		}
		bi, ok = ast.BuiltinMap[name]
		if !ok {
			return errors.New("builtin not found: " + name)
		}
	}

	if bi.IsNondeterministic() && state.Globals.NDBCache != nil {
		value, ok := state.Globals.NDBCache.Get(bi.Name, ast.NewArray(a...))
		// NOTE(sr): Nondet builtins currently aren't relations, and don't return void.
		if ok {
			v, err := state.ValueOps().FromInterface(state.Globals.Ctx, value)
			if err != nil {
				return err
			}
			state.SetReturnValue(Unused, v)
			return nil
		}
	}

	if err := impl(bctx, a,
		func(value *ast.Term) error {
			if relation {
				v, err := state.ValueOps().FromInterface(state.Globals.Ctx, value.Value)
				if err != nil {
					return err
				}

				newArray, ok, err := state.ValueOps().ArrayAppend(state.Globals.Ctx, state.Local(Unused), v)
				if err != nil {
					return err
				}
				if ok {
					state.SetValue(Unused, newArray)
				}
			} else {
				// topdown print returns iter(nil)
				var ret Value
				if value != nil {
					var err error
					ret, err = state.ValueOps().FromInterface(state.Globals.Ctx, value.Value)
					if err != nil {
						return err
					}
					if state.Globals.NDBCache != nil && bi.IsNondeterministic() {
						state.Globals.NDBCache.Put(bi.Name, ast.NewArray(a...), value.Value)
					}
				} else {
					ret = state.ValueOps().MakeArray(0)
				}
				state.SetReturnValue(Unused, ret)
			}
			return nil
		}); err != nil {
		var t topdown.Halt
		if errors.As(err, &t) {
			return err
		}
		state.Globals.BuiltinErrors = append(state.Globals.BuiltinErrors, err)
	}

	return nil
}

func (b block) Execute(state *State) (bool, uint32, error) {
	var stop bool
	var index uint32
	var err error
	var instr int64

	statements := b.Statements()
	statement := statements.Statement()

	for i, n := 0, statements.Len(); !stop && err == nil && i < n; i++ {
		// Check for context cancellation and instruction
		// limit breach every 32th statement.
		if i%32 == 0 {
			err = state.Instr(instr)
			instr = 0

			if err != nil {
				break
			}
		}

		// fmt.Printf("executing %d/%d: %T\n", i, len(b.Statements), b.Statements[i])
		var size int
		stop, index, size, err = statement.Execute(state)
		statement = statement[size:]
		instr++
	}

	err2 := state.Instr(instr)

	if err != nil {
		return false, 0, err
	} else if err2 != nil {
		return false, 0, err2
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

type exec interface {
	Execute(*State) (stop bool, index uint32, err error)
}

type N interface {
	~[]byte
	exec
}

func n[T N](xs []byte, s *State) (bool, uint32, error) {
	return T(xs).Execute(s)
}

var exs = [...]func([]byte, *State) (bool, uint32, error){
	typeStatementArrayAppend:      n[arrayAppend],
	typeStatementAssignInt:        n[assignInt],
	typeStatementAssignVar:        n[assignVar],
	typeStatementAssignVarOnce:    n[assignVarOnce],
	typeStatementBlockStmt:        n[blockStmt],
	typeStatementBreakStmt:        n[breakStmt],
	typeStatementCall:             n[call],
	typeStatementCallDynamic:      n[callDynamic],
	typeStatementDot:              n[dot],
	typeStatementEqual:            n[equal],
	typeStatementIsArray:          n[isArray],
	typeStatementIsDefined:        n[isDefined],
	typeStatementIsObject:         n[isObject],
	typeStatementIsUndefined:      n[isUndefined],
	typeStatementLen:              n[lenStmt],
	typeStatementMakeArray:        n[makeArray],
	typeStatementMakeNull:         n[makeNull],
	typeStatementMakeNumberInt:    n[makeNumberInt],
	typeStatementMakeNumberRef:    n[makeNumberRef],
	typeStatementMakeObject:       n[makeObject],
	typeStatementMakeSet:          n[makeSet],
	typeStatementNop:              n[nop],
	typeStatementNot:              n[not],
	typeStatementNotEqual:         n[notEqual],
	typeStatementObjectInsert:     n[objectInsert],
	typeStatementObjectInsertOnce: n[objectInsertOnce],
	typeStatementObjectMerge:      n[objectMerge],
	typeStatementResetLocal:       n[resetLocal],
	typeStatementResultSetAdd:     n[resultSetAdd],
	typeStatementReturnLocal:      n[returnLocal],
	typeStatementScan:             n[scan],
	typeStatementSetAdd:           n[setAdd],
	typeStatementWith:             n[with],
	// ...
	64: nil,
}

func (s statement) Execute(state *State) (stop bool, index uint32, size int, err error) {
	var t uint32
	t, size = s.Type()
	t = t & 63
	ex := exs[t]
	stop, index, err = ex(s, state)
	return
}

func (nop) Execute(*State) (bool, uint32, error) {
	return false, 0, nil
}

func (a assignInt) Execute(state *State) (bool, uint32, error) {
	state.SetValue(a.Target(), state.ValueOps().MakeNumberInt(a.Value()))
	return false, 0, nil
}

func (a assignVarOnce) Execute(state *State) (bool, uint32, error) {
	target, source := a.Target(), a.Source()

	targetValue := state.Local(target)
	if !isUndefinedType(targetValue) {
		sourceValue := state.Value(source)
		if isUndefinedType(sourceValue) {
			return false, 0, ErrVarAssignConflict
		}

		if eq, err := state.ValueOps().Equal(state.Globals.Ctx, sourceValue, targetValue); err != nil {
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
	return false, 0, nil
}

func (s scan) Execute(state *State) (bool, uint32, error) {
	var stop bool
	var n uint32

	// TODO: Should break index=1 if the source is not iterable?

	source, skey, svalue := s.Source(), s.Key(), s.Value()
	block := s.Block()

	if err2 := func(f func(key, value interface{}) (bool, error)) error {
		return state.ValueOps().Iter(state.Globals.Ctx, state.Local(source), *noescape(&f))
	}(func(key, value interface{}) (bool, error) {
		state.SetValue(skey, key)
		state.SetValue(svalue, value)

		var err error
		stop, n, err = block.Execute(state)
		if stop || err != nil {
			return true, err
		}

		return false, nil
	}); err2 != nil {
		return false, 0, err2
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
	var err error

	blocks := b.Blocks()
	for i, m := 0, blocks.Len(); i < m && err == nil && !stop; i++ {
		stop, n, err = blocks.Block(i).Execute(state)
	}

	// Block statement is not considered a nested block and hence
	// do not decrement the index.

	return stop, n, err
}

func (b breakStmt) Execute(*State) (bool, uint32, error) {
	return true, b.Index(), nil
}

func (n not) Execute(state *State) (bool, uint32, error) {
	var stop bool
	var index uint32
	var err error

	statements := n.Block().Statements()
	statement := statements.Statement()

	for i, m := 0, statements.Len(); !stop && err == nil && i < m; i++ {
		var size int
		stop, index, size, err = statement.Execute(state)
		statement = statement[size:]
	}

	if err != nil {
		return false, 0, err
	}

	if stop {
		if index == 0 {
			return false, 0, nil
		}

		return true, index - 1, nil
	}

	return true, 0, nil
}

func (r returnLocal) Execute(state *State) (bool, uint32, error) {
	state.SetReturn(r.Source())
	return false, 0, nil
}

func (call callDynamic) Execute(state *State) (bool, uint32, error) {
	inner := state.New()
	defer inner.Release()

	args := inner.Args(int(call.ArgsLen()))
	call.ArgsIter(func(i uint32, arg Local) error {
		args[i] = state.Local(arg)
		return nil
	})

	var path []string
	if n := call.PathLen(); n <= 4 {
		path = make([]string, n, 4)
	} else {
		path = make([]string, n)
	}
	if err := call.PathIter(func(i uint32, arg LocalOrConst) error {
		s, err := state.ValueOps().ToAST(state.Globals.Ctx, state.Value(arg))
		if err != nil {
			return err
		}

		path[i] = string(s.(ast.String))
		return nil
	}); err != nil {
		return false, 0, err
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

	resultValue := inner.Local(result)
	state.SetValue(call.Result(), resultValue)
	return false, 0, nil
}

func externalCall(state *State, path []string, args []Value) (interface{}, bool, bool, error) {
	if len(path) > 0 {
		path = path[1:]
	}

	if !state.IsLocalDefined(Data) {
		return nil, false, false, nil
	}

	data := state.Local(Data)
	a := make([]*interface{}, len(args))
	for i := range a {
		if !isUndefinedType(args[i]) {
			a[i] = (*interface{})(&args[i])
		}
	}

	for _, seg := range path {
		s := state.ValueOps().MakeString(seg)
		value, defined, err := state.ValueOps().GetCall(state.Globals.Ctx, data, s)
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
	inner := state.New()
	defer inner.Release()

	args := inner.Args(int(call.ArgsLen()))
	call.ArgsIter(func(i uint32, arg LocalOrConst) error {
		args[i] = state.Value(arg)
		return nil
	})

	if err := state.Func(call.Func()).Execute(inner, args); err != nil {
		return false, 0, err
	}

	result, ok := inner.Return()
	if !ok {
		return true, 0, nil
	}

	resultValue := inner.Local(result)
	state.SetValue(call.Result(), resultValue)
	return false, 0, nil
}

func (d dot) Execute(state *State) (bool, uint32, error) {
	source := d.Source()

	switch source.Type() {
	case boolConstType, stringIndexConstType: // can't dot these
		return true, 0, nil
	}

	src := source.Local()
	sourceValue := state.Local(src)
	target := d.Target()

	if isUndefinedType(sourceValue) {
		state.Unset(target)
		return true, 0, nil
	}

	keyValue := state.Value(d.Key())
	if isUndefinedType(keyValue) {
		state.Unset(target)
		return true, 0, nil
	}

	get := state.ValueOps().Get
	data := state.IsData(src)
	if data {
		get = state.DataGet
	}

	if value, ok, err := get(state.Globals.Ctx, sourceValue, keyValue); err != nil {
		return false, 0, err
	} else if ok {
		state.SetValue(target, value)
		if data {
			state.SetData(target)
		}

		return false, 0, nil
	}

	state.Unset(target)
	return true, 0, nil
}

func (e equal) Execute(state *State) (bool, uint32, error) {
	a, b := e.A(), e.B()
	aValue, bValue := state.Value(a), state.Value(b)
	definedA, definedB := !isUndefinedType(aValue), !isUndefinedType(bValue)

	switch {
	case !definedA && !definedB:
		return false, 0, nil

	case definedA && definedB:
		eq, err := state.ValueOps().Equal(state.Globals.Ctx, aValue, bValue)
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
	source := i.Source()
	sourceValue := state.Value(source)
	if defined := !isUndefinedType(sourceValue); !defined {
		return true, 0, nil
	}

	is, err := state.ValueOps().IsArray(state.Globals.Ctx, sourceValue)
	if err != nil {
		return false, 0, err
	}
	return !is, 0, nil
}

func (i isObject) Execute(state *State) (bool, uint32, error) {
	source := i.Source()
	sourceValue := state.Value(source)
	if defined := !isUndefinedType(sourceValue); !defined {
		return true, 0, nil
	}

	is, err := state.ValueOps().IsObject(state.Globals.Ctx, sourceValue)
	if err != nil {
		return false, 0, err
	}

	return !is, 0, nil
}

func (i isDefined) Execute(state *State) (bool, uint32, error) {
	return !state.IsLocalDefined(i.Source()), 0, nil
}

func (i isUndefined) Execute(state *State) (bool, uint32, error) {
	return state.IsLocalDefined(i.Source()), 0, nil
}

func (m makeNull) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeNull())
	return false, 0, nil
}

func (m makeNumberInt) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeNumberInt(m.Value()))
	return false, 0, nil
}

func (m makeNumberRef) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeNumberRef(state.String(StringIndexConst(m.Index()))))
	return false, 0, nil
}

func (m makeArray) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeArray(m.Capacity()))
	return false, 0, nil
}

func (m makeSet) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeSet())
	return false, 0, nil
}

func (m makeObject) Execute(state *State) (bool, uint32, error) {
	state.SetValue(m.Target(), state.ValueOps().MakeObject())
	return false, 0, nil
}

func (l lenStmt) Execute(state *State) (bool, uint32, error) {
	n, err := state.ValueOps().Len(state.Globals.Ctx, state.Value(l.Source()))
	if err == nil {
		state.SetValue(l.Target(), n)
	}

	return false, 0, err
}

func (a arrayAppend) Execute(state *State) (bool, uint32, error) {
	array, value := a.Array(), a.Value()
	newArray, ok, err := state.ValueOps().ArrayAppend(state.Globals.Ctx, state.Local(array), state.Value(value))
	if err != nil {
		return false, 0, err
	} else if ok {
		state.SetValue(array, newArray)
	}
	return false, 0, nil
}

func (s setAdd) Execute(state *State) (bool, uint32, error) {
	set := s.Set()
	newSet, err := state.ValueOps().SetAdd(state.Globals.Ctx, state.Local(set), state.Value(s.Value()))
	if err != nil {
		return false, 0, err
	}

	state.SetValue(set, newSet)
	return false, 0, nil
}

func (o objectInsertOnce) Execute(state *State) (bool, uint32, error) {
	ops := state.ValueOps()

	key, value, object := state.Value(o.Key()), state.Value(o.Value()), state.Local(o.Object())
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

	object, ok, err = ops.ObjectInsert(state.Globals.Ctx, object, key, value)
	if ok && err == nil {
		state.SetValue(o.Object(), object)
	}
	return false, 0, err
}

func (o objectInsert) Execute(state *State) (bool, uint32, error) {
	key, value, object := state.Value(o.Key()), state.Value(o.Value()), state.Local(o.Object())
	object, ok, err := state.ValueOps().ObjectInsert(state.Globals.Ctx, object, key, value)
	if ok && err == nil {
		state.SetValue(o.Object(), object)
	}
	return false, 0, err
}

func (o objectMerge) Execute(state *State) (bool, uint32, error) {
	ca, cb, target := o.A(), o.B(), o.Target()
	a, b := state.Local(ca), state.Local(cb)

	if isUndefinedType(a) {
		state.SetLocal(target, cb)
		return false, 0, nil
	}

	if isUndefinedType(b) {
		state.SetLocal(target, ca)
		return false, 0, nil
	}

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

	m, err := ops.ObjectMerge(state.Globals.Ctx, a, b)
	if err != nil {
		return false, 0, err
	}

	state.SetValue(target, m)
	return false, 0, nil
}

func (r resetLocal) Execute(state *State) (bool, uint32, error) {
	state.Unset(r.Target())
	return false, 0, nil
}

func (r resultSetAdd) Execute(state *State) (bool, uint32, error) {
	value := r.Value()
	valueValue := state.Local(value)
	if isUndefinedType(valueValue) {
		return false, 0, nil
	}

	newSet, err := state.ValueOps().SetAdd(state.Globals.Ctx, state.Globals.ResultSet, valueValue)
	if err != nil {
		return false, 0, err
	}

	state.Globals.ResultSet = newSet.(fjson.Set)
	return false, 0, nil
}

func (with with) Execute(state *State) (bool, uint32, error) {
	state.MemoizePush()
	defer state.MemoizePop()

	local, wvalue := with.Local(), with.Value()

	value := state.Local(local)
	defer state.SetValue(local, value)

	pathLen := with.PathLen()
	if pathLen == 0 {
		state.Set(local, wvalue)
	} else {

		value, err := with.upsert(state, local, pathLen, wvalue)
		if err != nil {
			return false, 0, err
		}

		state.SetValue(local, value)
	}

	statements := with.Block().Statements()
	statement := statements.Statement()

	for i, n := 0, statements.Len(); i < n; i++ {
		stop, _, size, err := statement.Execute(state)
		if err != nil {
			return false, 0, err
		} else if stop {
			return stop, 0, nil
		}

		statement = statement[size:]
	}

	return false, 0, nil
}

func (with with) upsert(state *State, original Local, pathLen uint32, value LocalOrConst) (Value, error) {
	ops := state.ValueOps()

	var ok bool
	originalValue := state.Local(original)
	if !isUndefinedType(originalValue) {
		var err error
		ok, err = ops.IsObject(state.Globals.Ctx, originalValue)
		if err != nil {
			return nil, err
		}
	}

	var result Value
	var err error
	if ok {
		result, err = ops.CopyShallow(state.Globals.Ctx, originalValue)
	} else {
		result = ops.MakeObject()
	}
	if err != nil {
		return nil, err
	}

	// We insert sthe values/nested objects into their parents
	// starting from the leaf: this accommodates the insert
	// operations which may return a new object instance.

	insertToParent := func(obj Value) error {
		result = obj
		return nil
	}

	obj := result
	pathLen-- // upto last item
	if err := with.PathIter(func(i uint32, arg int) error {
		key := state.String(StringIndexConst(arg))

		if i == pathLen {
			obj, _, err := ops.ObjectInsert(state.Globals.Ctx, obj, key, state.Value(value))
			if err != nil {
				return err
			}

			return insertToParent(obj)
		}

		child, ok, err := ops.Get(state.Globals.Ctx, obj, key)
		if err != nil {
			return err
		}

		var isObject bool
		if !ok {
			child = ops.MakeObject()
		} else if isObject, err = ops.IsObject(state.Globals.Ctx, child); err != nil {
			return err
		} else if !isObject {
			child = ops.MakeObject()
		} else {
			child, err = ops.CopyShallow(state.Globals.Ctx, child)
		}

		if err != nil {
			return err
		}

		curr, i2p := obj, insertToParent
		insertToParent = func(child Value) error {
			curr, _, err := ops.ObjectInsert(state.Globals.Ctx, curr, key, child)
			if err != nil {
				return err
			}

			return i2p(curr)
		}

		obj = child
		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

// noescape hides a pointer from escape analysis.  noescape is
// the identity function but escape analysis doesn't think the
// output depends on the input.  noescape is inlined and currently
// compiles down to zero instructions.
// USE CAREFULLY!
//
//go:nosplit
func noescape[T any](t *T) *T {
	p := unsafe.Pointer(t)
	x := uintptr(p)
	return (*T)(unsafe.Pointer(x ^ 0)) //nolint:staticcheck
}
