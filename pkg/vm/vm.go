// vm implements Rego interpreter evaluating compiled Rego IR.
package vm

import (
	"context"
	gjson "encoding/json"
	"errors"
	"io"
	gstrings "strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"
	"github.com/open-policy-agent/opa/topdown/print"
	"github.com/open-policy-agent/opa/tracing"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

var (
	ErrVarAssignConflict         = errors.New("var assignment conflict")
	ErrObjectInsertConflict      = errors.New("object insert conflict")
	ErrFunctionCallToData        = errors.New("function call to data")
	ErrInvalidExecutable         = errors.New("invalid executable")
	ErrQueryNotFound             = errors.New("query not found")
	ErrInstructionsLimitExceeded = errors.New("instructions limit exceeded")

	DefaultLimits = Limits{
		Instructions: 100000000,
	}
)

type (
	VM struct {
		executable   Executable
		data         *interface{}
		ops          DataOperations
		stringsCache []atomic.Pointer[fjson.String]
	}

	EvalOpts struct {
		Time                   time.Time
		PrintHook              print.Hook
		Metrics                metrics.Metrics
		Seed                   io.Reader
		Runtime                interface{}
		InterQueryBuiltinCache cache.InterQueryCache
		Input                  *interface{} // Input as golang native data.
		Limits                 *Limits
		Cache                  builtins.Cache
		NDBCache               builtins.NDBCache
		Capabilities           *ast.Capabilities
		BuiltinFuncs           map[string]*topdown.Builtin
		TracingOpts            tracing.Options
		StrictBuiltinErrors    bool
	}

	// State holds all the evaluation state and is passed along the statements as the evaluation progresses.
	State struct {
		Globals *Globals
		stats   *Statistics
		locals  Locals
	}

	Globals struct {
		registersPool          sync.Pool
		statePool              sync.Pool
		Time                   time.Time
		Metrics                metrics.Metrics
		PrintHook              print.Hook
		InterQueryBuiltinCache cache.InterQueryCache
		Ctx                    context.Context
		Seed                   io.Reader
		Runtime                *ast.Term
		vm                     *VM
		Cache                  builtins.Cache
		BuiltinFuncs           map[string]*topdown.Builtin
		Capabilities           *ast.Capabilities
		Input                  *interface{}
		NDBCache               builtins.NDBCache
		BuiltinErrors          []error
		TracingOpts            tracing.Options
		memoize                []map[int]Value
		Limits                 Limits
		StrictBuiltinErrors    bool
		cancel                 cancel
		ResultSet              Set
	}

	Limits struct {
		Instructions int64
	}

	Locals struct {
		registers registersList
		data      bitset
		ret       Local // function return value
	}

	registersList struct {
		next      *registersList // linked list of sync.Pool objects
		registers [registersSize]Value
	}

	Value interface{}

	Local        int
	LocalOrConst struct {
		t int
		v int
	}

	BoolConst        bool
	StringIndexConst int

	cancel struct {
		value int32
		exit  chan struct{}
	}
)

func NewLocal(v int) LocalOrConst {
	return LocalOrConst{localType, v}
}

func NewBoolConst(v bool) LocalOrConst {
	if v {
		return LocalOrConst{boolConstType, 1}
	}

	return LocalOrConst{boolConstType, 0}
}

func NewStringIndexConst(v int) LocalOrConst {
	return LocalOrConst{stringIndexConstType, v}
}

func (l *LocalOrConst) Type() int {
	return l.t
}

func (l *LocalOrConst) Local() Local {
	return Local(l.v)
}

func (l *LocalOrConst) BoolConst() BoolConst {
	return BoolConst(l.v != 0)
}

func (l *LocalOrConst) StringIndexConst() StringIndexConst {
	return StringIndexConst(l.v)
}

const (
	// Input is the local variable that refers to the global input document.
	Input  Local = iota
	Data         // Data is the local variable that refers to the global data document.
	Unused       // Unused is the free local variable that can be allocated in a plan.
)

// registersSize: tradeoff between pre-allocating too much and not enough; tested values 4, 8 and 16
const registersSize int = 32

func newGlobals(ctx context.Context, vm *VM, opts EvalOpts, runtime *ast.Term, input *interface{}) (*Globals, *cancel) {
	g := &Globals{
		vm:                  vm,
		Limits:              *opts.Limits,
		memoize:             []map[int]Value{{}},
		Ctx:                 ctx,
		Input:               input,
		Metrics:             opts.Metrics,
		Time:                opts.Time,
		Seed:                opts.Seed,
		Runtime:             runtime,
		PrintHook:           opts.PrintHook,
		StrictBuiltinErrors: opts.StrictBuiltinErrors,
		NDBCache:            opts.NDBCache,
		Capabilities:        opts.Capabilities,
		TracingOpts:         opts.TracingOpts,
		BuiltinFuncs:        opts.BuiltinFuncs,
		registersPool: sync.Pool{
			New: newRegisterPoolElement,
		},
		statePool: sync.Pool{
			New: newStateElement,
		},
	}

	g.cancel.Init(ctx)
	return g, &g.cancel
}

func newRegisterPoolElement() any {
	l := new(registersList)
	for i := range l.registers {
		l.registers[i] = undefined()
	}
	return l
}

func newStateElement() any {
	return &State{}
}

func NewVM() *VM {
	return &VM{}
}

func (vm *VM) WithExecutable(executable Executable) *VM {
	vm.executable = executable
	vm.stringsCache = make([]atomic.Pointer[fjson.String], executable.Strings().Len())
	return vm
}

// WithDataNamespace hooks an external namespace implementation to use
// as 'data.'.
func (vm *VM) WithDataNamespace(data interface{}) *VM {
	vm.data = &data
	return vm
}

// WithDataJSON stores golang native data for the evaluation to use as
// 'data.'.
func (vm *VM) WithDataJSON(data interface{}) *VM {
	vm.data = &data
	return vm
}

// Eval evaluates the query with the options given. Eval is thread
// safe. Return value is of ast.Value for now.
func (vm *VM) Eval(ctx context.Context, name string, opts EvalOpts) (ast.Value, error) {
	if !vm.executable.IsValid() {
		return nil, ErrInvalidExecutable
	}

	plans := vm.executable.Plans()
	n := plans.Len()

	for i := 0; i < n; i++ {
		if plan := plans.Plan(i); plan.Name() == name {

			var input *interface{}
			if opts.Input != nil {
				var err error
				var i interface{}
				i, err = vm.ops.FromInterface(ctx, *opts.Input)
				if err != nil {
					return nil, err
				}
				input = &i
			}

			if opts.Time.IsZero() {
				opts.Time = time.Now()
			}

			if opts.Limits == nil {
				opts.Limits = &DefaultLimits
			}

			runtime, err := vm.runtime(ctx, opts.Runtime)
			if err != nil {
				return nil, err
			}

			globals, cancel := newGlobals(ctx, vm, opts, runtime, input)
			defer cancel.Exit()

			globals.ResultSet = *vm.ops.MakeSet()
			globals.Cache = opts.Cache
			globals.InterQueryBuiltinCache = opts.InterQueryBuiltinCache

			state := newState(globals, StatisticsGet(ctx))
			defer state.Release()

			globals.Ctx = context.WithValue(globals.Ctx, regoEvalOptsContextKey{}, EvalOpts{
				Limits:              &globals.Limits, // TODO: Relay the instruction count.
				Metrics:             globals.Metrics,
				Time:                globals.Time,
				Seed:                globals.Seed,
				Runtime:             globals.Runtime,
				PrintHook:           globals.PrintHook,
				StrictBuiltinErrors: globals.StrictBuiltinErrors,
				NDBCache:            globals.NDBCache,
				Capabilities:        globals.Capabilities,
			})
			globals.Ctx = context.WithValue(globals.Ctx, regoEvalNamespaceContextKey{}, vm.data)

			if err := plan.Execute(state); err != nil {
				return nil, err
			}

			if opts.StrictBuiltinErrors && len(globals.BuiltinErrors) > 0 {
				return nil, globals.BuiltinErrors[0]
			}

			return vm.ops.ToAST(ctx, &globals.ResultSet)
		}
	}

	return nil, ErrQueryNotFound
}

func (vm *VM) Function(ctx context.Context, path []string, opts EvalOpts) (Value, bool, bool, error) {
	if !vm.executable.IsValid() {
		return nil, false, false, ErrInvalidExecutable
	}

	fname := "g0.data"
	if len(path) > 0 {
		fname += "." + gstrings.Join(path, ".")
	}

	// Try finding a function first, since their execution is a
	// bit cheaper due to lack of result wrapping.

	functions := vm.executable.Functions()
	n := functions.Len()

	for i := 0; i < n; i++ {
		if f := functions.Function(i); f.Name() == fname {
			if opts.Time.IsZero() {
				opts.Time = time.Now()
			}

			if opts.Limits == nil {
				opts.Limits = &DefaultLimits
			}

			runtime, err := vm.runtime(ctx, opts.Runtime)
			if err != nil {
				return nil, false, false, err
			}

			globals, cancel := newGlobals(ctx, vm, opts, runtime, opts.Input)
			defer cancel.Exit()

			args := make([]Value, 2)
			if opts.Input != nil {
				var v Value = *opts.Input
				args[0] = v
			} else {
				args[0] = undefined()
			}

			if vm.data != nil {
				var v Value = *vm.data
				args[1] = v
			} else {
				args[1] = undefined()
			}

			state := newState(globals, StatisticsGet(ctx))
			defer state.Release()
			if err := f.Execute(state, args); err != nil {
				return nil, false, false, err
			}

			if opts.StrictBuiltinErrors && len(globals.BuiltinErrors) > 0 {
				return nil, false, false, globals.BuiltinErrors[0]
			}

			result, ok := state.Return()
			if !ok {
				// undefined
				return nil, false, true, nil
			}

			return state.Local(result), true, true, nil
		}
	}

	pname := gstrings.Join(path, "/")
	plans := vm.executable.Plans()
	n = plans.Len()

	for i := 0; i < n; i++ {
		if plan := plans.Plan(i); plan.Name() == pname {
			if opts.Time.IsZero() {
				opts.Time = time.Now()
			}

			if opts.Limits == nil {
				opts.Limits = &DefaultLimits
			}

			runtime, err := vm.runtime(ctx, opts.Runtime)
			if err != nil {
				return nil, false, false, err
			}

			globals, cancel := newGlobals(ctx, vm, opts, runtime, opts.Input)
			defer cancel.Exit()

			globals.ResultSet = *vm.ops.MakeSet()

			state := newState(globals, StatisticsGet(ctx))
			defer state.Release()
			if err := plan.Execute(state); err != nil {
				return nil, false, false, err
			}

			if opts.StrictBuiltinErrors && len(globals.BuiltinErrors) > 0 {
				return nil, false, false, globals.BuiltinErrors[0]
			}

			var m interface{}
			globals.ResultSet.Iter(func(v fjson.Json) bool {
				m = v
				return true
			})

			r, defined, err := vm.ops.Get(ctx, m, vm.ops.MakeString("result"))
			return r, defined, true, err
		}
	}

	return nil, false, false, nil
}

func (vm *VM) runtime(ctx context.Context, v interface{}) (*ast.Term, error) {
	var runtime ast.Value
	if v != nil {
		switch v := v.(type) {
		case ast.Value:
			runtime = v
		case *ast.Term:
			runtime = v.Value
		default:
			ok, err := vm.ops.IsObject(ctx, v)
			if err != nil {
				return nil, err
			}
			if !ok {
				v, err = vm.ops.FromInterface(ctx, v)
				if err != nil {
					return nil, err
				}
			}

			runtime, err = vm.ops.ToAST(ctx, v)
			if err != nil {
				return nil, err
			}
		}
	} else {
		runtime = ast.NewObject()
	}

	return ast.NewTerm(runtime), nil
}

func (vm *VM) getCachedString(i StringIndexConst) (Value, bool) {
	if int(i) >= len(vm.stringsCache) {
		return nil, false
	}

	value := vm.stringsCache[int(i)].Load()
	return value, value != nil
}

func (vm *VM) setCachedString(i StringIndexConst, value *fjson.String) {
	vm.stringsCache[int(i)].Store(value)
}

type bitset struct {
	rest []bool
	base uint64
}

const baseBits = 64

func (b *bitset) set(i int, v bool) {
	if i < baseBits {
		if v {
			b.base |= (1 << i)
		} else {
			b.base &= ^(1 << i)
		}
		return
	}

	i -= baseBits

	if l := len(b.rest); l <= i {
		if !v {
			return
		}

		b.rest = append(b.rest, make([]bool, i-l+1)...)
	}

	b.rest[i] = v
}

func (b *bitset) isSet(i int) bool {
	if i < baseBits {
		return (b.base & (1 << i)) > 0
	}

	i -= baseBits

	if l := len(b.rest); l <= i {
		return false
	}

	return b.rest[i]
}

func newState(globals *Globals, stats *Statistics) *State {
	s := globals.statePool.Get().(*State)

	s.Globals = globals
	s.locals = Locals{ret: -1}
	s.stats = stats

	s.locals.data.set(int(Data), true)

	if globals.Input != nil {
		s.SetValue(Input, *globals.Input)
	}

	if globals.vm.data != nil {
		s.SetValue(Data, *globals.vm.data)
	}
	return s
}

func (s *State) Release() {
	p := s.locals.registers.next
	var next *registersList
	for ; p != nil; p = next {
		for i := range p.registers {
			p.registers[i] = undefined() // release Values
		}
		next = p.next
		p.next = nil
		s.Globals.registersPool.Put(p) //nolint: staticcheck
	}

	s.Globals.statePool.Put(s)
}

func (s *State) New() *State {
	return newState(s.Globals, s.stats)
}

func (s *State) ValueOps() *DataOperations {
	return &s.Globals.vm.ops
}

func (s *State) Func(f int) function {
	return s.Globals.vm.executable.Functions().Function(f)
}

func (s *State) FindByPath(path []string) (function, int) {
	functions := s.Globals.vm.executable.Functions()

next:
	for i := 0; i < functions.Len(); i++ {
		function := functions.Function(i)
		if function.IsBuiltin() {
			continue
		}

		if len(path) == int(function.PathLen()) {
			if err := function.PathIter(func(i uint32, arg string) error {
				if path[i] != arg {
					return ErrQueryNotFound // actual error is ignored
				}
				return nil
			}); err != nil {
				continue next
			}

			return function, i
		}
	}

	return nil, -1
}

func (s *State) IsDefined(v LocalOrConst) bool {
	switch v.Type() {
	case localType:
		v := v.Local()
		return !isUndefinedType(s.findReg(v).registers[int(v)%registersSize])
	default:
		return true
	}
}

func (s *State) IsLocalDefined(v Local) bool {
	return !isUndefinedType(s.findReg(v).registers[int(v)%registersSize])
}

func (s *State) Value(v LocalOrConst) Value {
	switch v.Type() {
	case localType:
		v := v.Local()
		return s.findReg(v).registers[int(v)%registersSize]
	case boolConstType:
		return s.ValueOps().MakeBoolean(bool(v.BoolConst()))
	case stringIndexConstType:
		return s.Globals.vm.executable.Strings().String(s.Globals.vm, v.StringIndexConst())
	}

	return nil
}

func (s *State) Local(v Local) Value {
	return s.findReg(v).registers[int(v)%registersSize]
}

func (s *State) String(v StringIndexConst) Value {
	return s.Globals.vm.executable.Strings().String(s.Globals.vm, v)
}

// Return returns the variable holding the function result.
func (s *State) Return() (Local, bool) {
	ret := s.locals.ret
	return ret, ret >= 0
}

func (s *State) Set(target Local, source LocalOrConst) {
	switch source.Type() {
	case localType:
		v := source.Local()
		r1, r2 := s.find2Regs(target, v)
		r1.registers[int(target)%registersSize] = r2.registers[int(v)%registersSize]

	case boolConstType:
		v := source.BoolConst()
		s.findReg(target).registers[int(target)%registersSize] = s.Globals.vm.ops.MakeBoolean(bool(v))

	case stringIndexConstType:
		v := source.StringIndexConst()
		s.findReg(target).registers[int(target)%registersSize] = s.Globals.vm.executable.Strings().String(s.Globals.vm, v)
	}
}

func (s *State) SetLocal(target Local, source Local) {
	v := source
	r1, r2 := s.find2Regs(target, v)
	r1.registers[int(target)%registersSize] = r2.registers[int(v)%registersSize]
}

func (s *State) SetReturn(source Local) {
	s.locals.SetReturn(source, !isUndefinedType(s.findReg(source).registers[int(source)%registersSize]))
}

func (s *State) SetReturnValue(source Local, value Value) {
	s.locals.SetReturn(source, true)
	s.SetValue(source, value)
}

func (s *State) SetValue(target Local, value Value) {
	s.findReg(target).registers[int(target)%registersSize] = value
}

func (s *State) SetData(l Local) {
	s.locals.data.set(int(l), true)
}

func (s *State) IsData(l Local) bool {
	return s.locals.data.isSet(int(l))
}

func (s *State) DataGet(ctx context.Context, value, key interface{}) (interface{}, bool, error) {
	if y, ok, err := s.ValueOps().Get(ctx, value, key); ok || err != nil {
		return y, ok, err
	}
	x, err := s.ValueOps().ToInterface(ctx, key)
	if err != nil {
		return nil, false, err
	}
	switch x := x.(type) {
	case gjson.Number:
		return s.ValueOps().Get(ctx, value, s.ValueOps().MakeString(string(x)))
	}
	return nil, false, nil
}

func (s *State) Unset(target Local) {
	s.findReg(target).registers[int(target)%registersSize] = undefined()
	s.locals.data.set(int(target), false)
}

func (s *State) MemoizePush() {
	s.Globals.memoize = append(s.Globals.memoize, map[int]Value{})
}

func (s *State) MemoizePop() {
	s.Globals.memoize = s.Globals.memoize[0 : len(s.Globals.memoize)-1]
}

func (s *State) MemoizeGet(idx int) (Value, bool) {
	v, ok := s.Globals.memoize[len(s.Globals.memoize)-1][idx]
	return v, ok
}

func (s *State) MemoizeInsert(idx int, value Value) {
	s.Globals.memoize[len(s.Globals.memoize)-1][idx] = value
}

func (s *State) Instr(i int64) error {
	instructions := s.stats.EvalInstructions

	if s.Globals.cancel.Cancelled() {
		return context.Canceled

	}
	if instructions > s.Globals.Limits.Instructions {
		// TODO: Consider using context.WithCancelCause.
		return ErrInstructionsLimitExceeded
	}

	s.stats.EvalInstructions = instructions + i

	return nil
}

func (l *Locals) SetReturn(source Local, defined bool) {
	if defined {
		l.ret = source
	} else {
		l.ret = -1
	}
}

func (s *State) findReg(v Local) *registersList {
	buckets := int(v) / registersSize
	r := &s.locals.registers
	for i := 0; i < buckets; i++ {
		if r.next == nil {
			r.next = s.Globals.registersPool.Get().(*registersList)
		}
		r = r.next
	}

	return r
}

func (s *State) find2Regs(v1, v2 Local) (*registersList, *registersList) {
	buckets1 := int(v1) / registersSize
	buckets2 := int(v2) / registersSize

	r := &s.locals.registers
	r1, r2 := r, r

	for i := 0; i < buckets1 || i < buckets2; {
		next := r.next
		if next == nil {
			next = s.Globals.registersPool.Get().(*registersList)
			r.next = next
		}

		r = next
		i++

		if i == buckets1 {
			r1 = next
		}

		if i == buckets2 {
			r2 = next
		}
	}

	return r1, r2
}

func (c *cancel) Init(ctx context.Context) {
	exit := make(chan struct{})
	c.exit = exit

	go c.wait(ctx, exit)
}

func (c *cancel) Cancel() {
	atomic.StoreInt32(&c.value, 1)
}

func (c *cancel) Cancelled() bool { // nolint:misspell // opa Cancel interface contains Cancelled function
	return atomic.LoadInt32(&c.value) != 0
}

func (c *cancel) Exit() {
	close(c.exit)
}

func (c *cancel) wait(ctx context.Context, exit chan struct{}) {
	select {
	case <-ctx.Done():
	case <-exit:
	}

	c.Cancel()
}

func EvalOptsFromContext(ctx context.Context) EvalOpts {
	return ctx.Value(regoEvalOptsContextKey{}).(EvalOpts)
}
