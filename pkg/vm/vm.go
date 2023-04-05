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
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"
	"github.com/open-policy-agent/opa/topdown/print"
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
		mu           sync.RWMutex
		stringsCache []Value
	}

	EvalOpts struct {
		Input                  *interface{} // Input as golang native data.
		Metrics                metrics.Metrics
		Time                   time.Time
		Seed                   io.Reader
		Runtime                interface{}
		InterQueryBuiltinCache cache.InterQueryCache
		PrintHook              print.Hook
		StrictBuiltinErrors    bool
		Limits                 *Limits
		Cache                  builtins.Cache
		NDBCache               builtins.NDBCache
		Capabilities           *ast.Capabilities
	}

	// State holds all the evaluation state and is passed along the statements as the evaluation progresses.
	State struct {
		Globals *Globals
		locals  Locals
		stats   *Statistics
	}

	Globals struct {
		vm                     *VM
		cancel                 *cancel
		Limits                 Limits
		memoize                []map[int]Value
		Ctx                    context.Context
		ResultSet              *Set
		Input                  *interface{}
		Metrics                metrics.Metrics
		Time                   time.Time
		Seed                   io.Reader
		Runtime                *ast.Term
		Cache                  builtins.Cache
		InterQueryBuiltinCache cache.InterQueryCache
		PrintHook              print.Hook
		StrictBuiltinErrors    bool
		BuiltinErrors          []error
		NDBCache               builtins.NDBCache
		registersPool          sync.Pool
		Capabilities           *ast.Capabilities
	}

	Limits struct {
		Instructions int64
	}

	Locals struct {
		registers *registersList
		data      bitset
		ret       Local // function return value
	}

	registersList struct {
		next      *registersList // linked list of sync.Pool objects
		registers [registersSize]Value
	}

	Value interface{}

	Local        int
	LocalOrConst interface {
		localOrConst()
	}

	BoolConst        bool
	StringIndexConst int

	cancel struct {
		value int32
	}
)

const (
	// Input is the local variable that refers to the global input document.
	Input  Local = iota
	Data         // Data is the local variable that refers to the global data document.
	Unused       // Unused is the free local variable that can be allocated in a plan.
)

// registersSize: tradeoff between pre-allocating too much and not enough; tested values 4, 8 and 16
const registersSize int = 32

func newGlobals(ctx context.Context, vm *VM, opts EvalOpts, cancel *cancel, runtime *ast.Term, input *interface{}) *Globals {
	return &Globals{
		vm:                  vm,
		Limits:              *opts.Limits,
		cancel:              cancel,
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
		registersPool: sync.Pool{
			New: func() any {
				l := new(registersList)
				for i := range l.registers {
					l.registers[i] = undefined()
				}
				return l
			},
		},
	}
}

func NewVM() *VM {
	return &VM{}
}

func (vm *VM) WithExecutable(executable Executable) *VM {
	vm.executable = executable
	vm.stringsCache = make([]Value, executable.Strings().Len())
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

	cancel, exit := watchContext(ctx)
	defer exit()

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

			result := vm.ops.MakeSet()

			globals := newGlobals(ctx, vm, opts, cancel, runtime, input)
			globals.ResultSet = result
			globals.Cache = opts.Cache
			globals.InterQueryBuiltinCache = opts.InterQueryBuiltinCache

			state := newState(globals, StatisticsGet(ctx))
			defer state.Release()
			if err := plan.Execute(&state); err != nil {
				return nil, err
			}

			if opts.StrictBuiltinErrors && len(globals.BuiltinErrors) > 0 {
				return nil, globals.BuiltinErrors[0]
			}

			return vm.ops.ToAST(ctx, result)
		}
	}

	return nil, ErrQueryNotFound
}

func (vm *VM) Function(ctx context.Context, path []string, opts EvalOpts) (Value, bool, bool, error) {
	if !vm.executable.IsValid() {
		return nil, false, false, ErrInvalidExecutable
	}

	cancel, exit := watchContext(ctx)
	defer exit()

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

			globals := newGlobals(ctx, vm, opts, cancel, runtime, opts.Input)

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
			if err := f.Execute(&state, args); err != nil {
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

			return state.Value(result), true, true, nil
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

			result := vm.ops.MakeSet()

			globals := newGlobals(ctx, vm, opts, cancel, runtime, opts.Input)
			globals.ResultSet = result

			state := newState(globals, StatisticsGet(ctx))
			if err := plan.Execute(&state); err != nil {
				return nil, false, false, err
			}

			if opts.StrictBuiltinErrors && len(globals.BuiltinErrors) > 0 {
				return nil, false, false, globals.BuiltinErrors[0]
			}

			var m interface{}
			if err := vm.ops.Iter(ctx, result, func(_, v interface{}) bool {
				m = v
				return true
			}); err != nil {
				return nil, false, false, err
			}

			v, err := vm.ops.FromInterface(ctx, "result")
			if err != nil {
				return nil, false, false, err
			}
			r, defined, err := vm.ops.Get(ctx, m, v)
			return r, defined, true, err
		}
	}

	return nil, false, false, nil
}

func (vm *VM) runtime(ctx context.Context, v interface{}) (*ast.Term, error) {
	var runtime ast.Value
	if v != nil {
		var ok bool
		runtime, ok = v.(ast.Value)
		if !ok {
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
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	if int(i) >= len(vm.stringsCache) {
		return nil, false
	}

	value := vm.stringsCache[int(i)]
	return value, value != nil
}

func (vm *VM) setCachedString(i StringIndexConst, value Value) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	vm.stringsCache[int(i)] = value
}

type bitset struct {
	base uint64
	rest []bool
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

func newState(globals *Globals, stats *Statistics) State {
	s := State{
		Globals: globals,
		locals:  Locals{ret: -1},
		stats:   stats,
	}
	s.locals.data.set(int(Data), true)
	s.locals.registers = s.Globals.registersPool.Get().(*registersList)

	if globals.Input != nil {
		s.SetValue(Input, *globals.Input)
	}

	if globals.vm.data != nil {
		s.SetValue(Data, *globals.vm.data)
	}
	return s
}

func (s *State) Release() {
	p := s.locals.registers
	var next *registersList
	for ; p != nil; p = next {
		for i := range p.registers {
			p.registers[i] = undefined() // release Values
		}
		next = p.next
		p.next = nil
		s.Globals.registersPool.Put(p) //nolint: staticcheck
	}
}

func (s *State) New() State {
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
	switch v := v.(type) {
	case Local:
		return !isUndefinedType(s.findReg(v).registers[int(v)%registersSize])
	default:
		return true
	}
}

func (s *State) Value(v LocalOrConst) Value {
	switch v := v.(type) {
	case Local:
		return s.findReg(v).registers[int(v)%registersSize]
	case BoolConst:
		return s.ValueOps().MakeBoolean(bool(v))
	case StringIndexConst:
		return s.Globals.vm.executable.Strings().String(s.Globals.vm, v)
	}

	return nil
}

// Return returns the variable holding the function result.
func (s *State) Return() (Local, bool) {
	ret := s.locals.ret
	return ret, ret >= 0
}

func (s *State) Set(target Local, source LocalOrConst) {
	switch v := source.(type) {
	case Local:
		s.findReg(target).registers[int(target)%registersSize] = s.findReg(v).registers[int(v)%registersSize]

	case BoolConst:
		s.findReg(target).registers[int(target)%registersSize] = s.Globals.vm.ops.MakeBoolean(bool(v))

	case StringIndexConst:
		s.findReg(target).registers[int(target)%registersSize] = s.Globals.vm.executable.Strings().String(s.Globals.vm, v)
	}
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
	r := s.locals.registers
	for i := 0; i < buckets; i++ {
		if r.next == nil {
			r.next = s.Globals.registersPool.Get().(*registersList)
		}
		r = r.next
	}

	return r
}

func (Local) localOrConst()            {}
func (BoolConst) localOrConst()        {}
func (StringIndexConst) localOrConst() {}

func watchContext(ctx context.Context) (*cancel, context.CancelFunc) {
	var c cancel

	exit := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-exit:
		}

		c.Cancel()
	}()

	return &c, func() { close(exit) }
}

func (c *cancel) Cancel() {
	atomic.StoreInt32(&c.value, 1)
}

func (c *cancel) Cancelled() bool { // nolint:misspell // opa Cancel interface contains Cancelled function
	return atomic.LoadInt32(&c.value) != 0
}
