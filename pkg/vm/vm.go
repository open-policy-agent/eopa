// vm implements Rego interpreter evaluating compiled Rego IR.
package vm

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"
	"github.com/open-policy-agent/opa/topdown/print"

	bjson "github.com/StyraInc/load/pkg/json"
)

var (
	ErrVarAssignConflict    = errors.New("var assignment conflict")
	ErrObjectInsertConflict = errors.New("object insert conflict")
	ErrFunctionCallToData   = errors.New("function call to data")
	ErrInvalidExecutable    = errors.New("invalid executable")
	ErrQueryNotFound        = errors.New("query not found")

	DefaultLimits = Limits{
		Instructions: 1000000,
	}
)

type (
	VM struct {
		executable Executable
		data       *interface{}
		ops        DataOperations
	}

	EvalOpts struct {
		Input                  *interface{} // Input as golang native data.
		Metrics                metrics.Metrics
		Time                   time.Time
		Seed                   io.Reader
		Runtime                bjson.Object
		InterQueryBuiltinCache cache.InterQueryCache
		PrintHook              print.Hook
		StrictBuiltinErrors    bool
		Limits                 *Limits
		Cache                  builtins.Cache
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
		memoize                []map[int]*Value
		Ctx                    context.Context
		ResultSet              Value
		Input                  *interface{}
		Metrics                metrics.Metrics
		Time                   time.Time
		Seed                   io.Reader
		Runtime                bjson.Object
		Cache                  builtins.Cache
		InterQueryBuiltinCache cache.InterQueryCache
		PrintHook              print.Hook
		StrictBuiltinErrors    bool
		BuiltinErrors          []error
	}

	Limits struct {
		Instructions int64
	}

	Locals struct {
		defined    []bool
		registers  []Value
		ret        Local // function return value
		retDefined bool
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
	Input Local = iota

	// Data is the local variable that refers to the global data document.
	Data

	// Unused is the free local variable that can be allocated in a plan.
	Unused
)

func NewVM() *VM {
	return &VM{}
}

func (vm *VM) WithExecutable(executable Executable) *VM {
	vm.executable = executable
	return vm
}

func (vm *VM) WithDataOperations(ops DataOperations) *VM {
	vm.ops = ops
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
func (vm *VM) Eval(ctx context.Context, name string, opts EvalOpts) (Value, error) {
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
				var i interface{} = vm.ops.FromInterface(*opts.Input)
				input = &i
			}

			if opts.Time.IsZero() {
				opts.Time = time.Now()
			}

			if opts.Limits == nil {
				opts.Limits = &DefaultLimits
			}

			result := vm.ops.MakeSet()

			globals := &Globals{
				vm:                     vm,
				cancel:                 cancel,
				Limits:                 *opts.Limits,
				memoize:                []map[int]*Value{{}},
				Ctx:                    ctx,
				ResultSet:              result,
				Input:                  input,
				Metrics:                opts.Metrics,
				Time:                   opts.Time,
				Seed:                   opts.Seed,
				Runtime:                opts.Runtime,
				Cache:                  opts.Cache,
				PrintHook:              opts.PrintHook,
				StrictBuiltinErrors:    opts.StrictBuiltinErrors,
				InterQueryBuiltinCache: opts.InterQueryBuiltinCache,
			}

			err := plan.Execute(newState(globals, StatisticsGet(ctx)))
			if err != nil {
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
	pname := ""

	for i, s := range path {
		fname += "."

		if i > 0 {
			pname += "/"
		}

		fname += s
		pname += s
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

			globals := &Globals{
				vm:                  vm,
				Limits:              *opts.Limits,
				cancel:              cancel,
				memoize:             []map[int]*Value{{}},
				Ctx:                 ctx,
				Input:               opts.Input, // TODO: This assumes it's already converted.
				Metrics:             opts.Metrics,
				Time:                opts.Time,
				Seed:                opts.Seed,
				PrintHook:           opts.PrintHook,
				StrictBuiltinErrors: opts.StrictBuiltinErrors,
			}

			args := make([]*Value, 2)
			if opts.Input != nil {
				var v Value = *opts.Input
				args[0] = &v
			}

			if vm.data != nil {
				var v Value = *vm.data
				args[1] = &v
			}

			state := newState(globals, StatisticsGet(ctx))
			err := f.Execute(state, args)
			if err != nil {
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

	plans := vm.executable.Plans()
	n = plans.Len()

	for i := 0; i < n; i++ {
		if plan := plans.Plan(i); plan.Name() == pname {
			args := make([]*Value, 2)
			if opts.Input != nil {
				var v Value = *opts.Input
				args[0] = &v
			}

			if vm.data != nil {
				var v Value = *vm.data
				args[1] = &v
			}

			if opts.Time.IsZero() {
				opts.Time = time.Now()
			}

			if opts.Limits == nil {
				opts.Limits = &DefaultLimits
			}

			result := vm.ops.MakeSet()

			globals := &Globals{
				vm:                  vm,
				cancel:              cancel,
				Limits:              *opts.Limits,
				memoize:             []map[int]*Value{{}},
				Ctx:                 ctx,
				ResultSet:           result,
				Input:               opts.Input,
				Metrics:             opts.Metrics,
				Time:                opts.Time,
				Seed:                opts.Seed,
				PrintHook:           opts.PrintHook,
				StrictBuiltinErrors: opts.StrictBuiltinErrors,
			}

			err := plan.Execute(newState(globals, StatisticsGet(ctx)))
			if err != nil {
				return nil, false, false, err
			}

			if opts.StrictBuiltinErrors && len(globals.BuiltinErrors) > 0 {
				return nil, false, false, globals.BuiltinErrors[0]
			}

			result = vm.ops.FromInterface(result)

			var m interface{}
			if err := vm.ops.Iter(ctx, result, func(_, v interface{}) bool {
				m = v
				return true
			}); err != nil {
				return nil, false, false, err
			}

			r, defined, err := vm.ops.Get(ctx, m, vm.ops.FromInterface("result"))
			return r, defined, true, err
		}
	}

	return nil, false, false, nil
}

func newState(globals *Globals, stats *Statistics) *State {
	s := State{
		Globals: globals,
		locals:  Locals{},
		stats:   stats,
	}

	if globals.Input != nil {
		s.SetValue(Input, *globals.Input)
	}

	if globals.vm.data != nil {
		s.SetValue(Data, *globals.vm.data)
	}
	return &s
}

func (s *State) New() *State {
	return newState(s.Globals, s.stats)
}

func (s *State) ValueOps() DataOperations {
	return s.Globals.vm.ops
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

		other := function.Path()

		if len(path) == len(other) {
			for i := 0; i < len(path); i++ {
				if path[i] != other[i] {
					continue next
				}
			}

			return function, i
		}
	}

	return nil, -1
}

func (s *State) IsDefined(v LocalOrConst) bool {
	switch v := v.(type) {
	case Local:
		s.locals.grow(v)
		return s.locals.defined[v]
	default:
		return true
	}
}

func (s *State) Value(v LocalOrConst) Value {
	switch v := v.(type) {
	case Local:
		s.locals.grow(v)
		return s.locals.registers[v]
	case BoolConst:
		return s.ValueOps().MakeBoolean(bool(v))
	case StringIndexConst:
		return s.Globals.vm.executable.Strings().String(s.Globals.vm.ops, v)
	}

	return nil
}

// Return returns the variable holding the function result.
func (s *State) Return() (Local, bool) {
	return s.locals.ret, s.locals.retDefined
}

func (s *State) Set(target Local, source LocalOrConst) {
	switch v := source.(type) {
	case Local:
		s.locals.grow(target)
		s.locals.grow(v)
		s.locals.defined[target] = s.locals.defined[v]
		s.locals.registers[target] = s.locals.registers[v]

	case BoolConst:
		s.locals.grow(target)
		s.locals.defined[target] = true
		s.locals.registers[target] = s.Globals.vm.ops.MakeBoolean(bool(v))

	case StringIndexConst:
		s.locals.grow(target)
		s.locals.defined[target] = true
		s.locals.registers[target] = s.Globals.vm.executable.Strings().String(s.Globals.vm.ops, v)
	}
}

func (s *State) SetFrom(target Local, other *State, source LocalOrConst) {
	switch v := source.(type) {
	case Local:
		s.locals.grow(target)
		other.locals.grow(v)
		s.locals.defined[target] = other.locals.defined[v]
		s.locals.registers[target] = other.locals.registers[v]

	case BoolConst:
		s.locals.grow(target)
		s.locals.defined[target] = true
		s.locals.registers[target] = s.Globals.vm.ops.MakeBoolean(bool(v))

	case StringIndexConst:
		s.locals.grow(target)
		s.locals.defined[target] = true
		s.locals.registers[target] = s.Globals.vm.executable.Strings().String(s.Globals.vm.ops, v)
	}
}

func (s *State) SetReturn(source Local) {
	s.locals.SetReturn(source)
}

func (s *State) SetValue(target Local, value Value) {
	s.locals.grow(target)
	s.locals.defined[target] = true
	s.locals.registers[target] = value
}

func (s *State) Unset(target Local) {
	s.locals.grow(target)
	s.locals.defined[target] = false
	s.locals.registers[target] = nil
}

func (s *State) MemoizePush() {
	s.Globals.memoize = append(s.Globals.memoize, map[int]*Value{})
}

func (s *State) MemoizePop() {
	s.Globals.memoize = s.Globals.memoize[0 : len(s.Globals.memoize)-1]
}

func (s *State) MemoizeGet(idx int) (*Value, bool) {
	v, ok := s.Globals.memoize[len(s.Globals.memoize)-1][idx]
	return v, ok
}

func (s *State) MemoizeInsert(idx int, value *Value) {
	s.Globals.memoize[len(s.Globals.memoize)-1][idx] = value
}

func (s *State) Instr() error {
	s.stats.EvalInstructions++

	instructions := s.stats.EvalInstructions

	if instructions%32 == 0 && s.Globals.cancel.Cancelled() {
		return context.Canceled
	}

	if instructions > s.Globals.Limits.Instructions {
		return context.Canceled
	}

	return nil
}

func (l *Locals) SetReturn(source Local) {
	l.ret = source
	l.retDefined = l.defined[source]
}

func (l *Locals) grow(v Local) {
	if n := int(v) - len(l.defined); n >= 0 {
		l.defined = append(l.defined, make([]bool, n+1)...)
		l.registers = append(l.registers, make([]Value, n+1)...)
	}
}

func (l Local) localOrConst()            {}
func (b BoolConst) localOrConst()        {}
func (s StringIndexConst) localOrConst() {}

func watchContext(ctx context.Context) (*cancel, func()) {
	var cancel cancel

	exit := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-exit:
		}

		cancel.Cancel()
	}()

	return &cancel, func() { close(exit) }
}

func (c *cancel) Cancel() {
	atomic.StoreInt32(&c.value, 1)
}

func (c *cancel) Cancelled() bool {
	return atomic.LoadInt32(&c.value) != 0
}
