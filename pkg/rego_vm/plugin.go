// Package rego_vm contains the rego target plugin to be used with OPA.
package rego_vm

import (
	"context"
	"crypto/rand"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/styrainc/enterprise-opa-private/pkg/iropt"
	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	_ "github.com/styrainc/enterprise-opa-private/pkg/plugins/bundle" // register bjson extension
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/impact"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
)

const (
	Name   = "rego_target_vm"
	Target = "vm"
)

var defaultTgt = false

func init() {
	rego.RegisterPlugin(Name, &vmp{})
}

var (
	limits    *vm.Limits
	limitsMtx sync.Mutex
)

// SetLimits allows changing the *vm.Limits passed to the VM with EvalOpts.
func SetLimits(instr int64) {
	limitsMtx.Lock()
	limits = &vm.Limits{Instructions: instr}
	limitsMtx.Unlock()
}

// SetDefault controls if "vm" assumes the role of the default rego target
func SetDefault(y bool) {
	defaultTgt = y
}

type vmp struct{}

// We want this target to be the default.
func (*vmp) IsTarget(t string) bool {
	return t == Target || defaultTgt
}

// Applies the current server-wide optimization schedule before building
// EOPA VM bytecode from the policy.
func (*vmp) PrepareForEval(_ context.Context, policy *ir.Policy, opts ...rego.PrepareOption) (rego.TargetPluginEval, error) {
	po := &rego.PrepareConfig{}
	for _, o := range opts {
		o(po)
	}

	var bis map[string]*topdown.Builtin
	bi, ok := any(po).(interface {
		BuiltinFuncs() map[string]*topdown.Builtin
	})
	if ok {
		bis = bi.BuiltinFuncs()
	}

	// Note(philip): This is where the IR optimization passes are applied.
	optimizedPolicy, err := iropt.RunPasses(policy, iropt.RegoVMIROptimizationPassSchedule)
	if err != nil {
		return nil, err
	}

	executable, err := vm.NewCompiler().WithPolicy(optimizedPolicy).WithBuiltins(bis).Compile()
	if err != nil {
		return nil, err
	}

	return &vme{
		builtinFuncs: bis,
		e:            executable,
	}, nil
}

type vme struct {
	builtinFuncs map[string]*topdown.Builtin
	e            vm.Executable
}

var tracer = otel.Tracer(Name)

func spanFromContext(ctx context.Context, query string) (ctx0 context.Context, span trace.Span) {
	ctx0, span = tracer.Start(ctx, "eval")
	span.SetAttributes(
		attribute.String("query", query),
	)
	return
}

func (t *vme) Eval(ctx context.Context, ectx *rego.EvalContext, rt ast.Value) (ast.Value, error) {
	v := vm.NewVM().WithExecutable(t.e)
	var span trace.Span
	ctx, span = spanFromContext(ctx, ectx.CompiledQuery().String())
	defer span.End()

	input := ectx.RawInput()
	if p := ectx.ParsedInput(); p != nil {
		i := interface{}(p)
		input = &i
	}

	ectx.Metrics().Timer(evalTimer).Start()
	var s *vm.Statistics
	s, ctx = vm.WithStatistics(ctx)

	seed := ectx.Seed()
	if seed == nil {
		seed = rand.Reader
	}

	// NOTE(sr): We're peeking into the transaction to cover cases where we've been fed a
	// default OPA inmem store, not an EOPA one. If that's the case, we'll read it in full,
	// and feed its data to the VM. That will have subtle differences in behavior; but it
	// is good enough for the remaining cases where this is allowed to happen: discovery
	// document evaluation.
	txn := ectx.Transaction()
	if txn0, ok := txn.(interface {
		Write(storage.PatchOp, storage.Path, any) error // only OPA's inmem txn has that
		Read(storage.Path) (any, error)                 // both EOPA's and OPA's inmem txn have that, using it below
	}); ok {
		x, err := txn0.Read(storage.Path{})
		if err != nil {
			return nil, err
		}
		y, err := bjson.New(x)
		if err != nil {
			return nil, err
		}
		v = v.WithDataJSON(y)
	} else {
		v = v.WithDataNamespace(txn)
	}

	// TODO(sr): Upstream this rego.EvalContext addition.
	var qt []topdown.QueryTracer
	if ectx, ok := any(ectx).(interface{ QueryTracers() []topdown.QueryTracer }); ok {
		qt = ectx.QueryTracers()
	}

	result, err := v.Eval(ctx, "eval", vm.EvalOpts{
		Metrics:                     ectx.Metrics(),
		Input:                       input,
		Time:                        ectx.Time(),
		Seed:                        seed,
		Runtime:                     rt,
		Cache:                       builtins.Cache{},
		NDBCache:                    ectx.NDBCache(),
		InterQueryBuiltinCache:      ectx.InterQueryBuiltinCache(),
		InterQueryBuiltinValueCache: ectx.InterQueryBuiltinValueCache(),
		PrintHook:                   ectx.PrintHook(),
		StrictBuiltinErrors:         ectx.StrictBuiltinErrors(),
		Capabilities:                ectx.Capabilities(),
		TracingOpts:                 tracingOpts(ectx),
		Limits:                      limits,
		BuiltinFuncs:                t.builtinFuncs,
		ExternalCancel:              getExternalCancel(ectx),
		QueryTracers:                qt,
	})
	ectx.Metrics().Timer(evalTimer).Stop()
	if err != nil {
		if err == vm.ErrVarAssignConflict {
			return nil, &topdown.Error{
				Code:    topdown.ConflictErr,
				Message: "complete rules must not produce multiple outputs",
			}
		}

		return nil, err
	}
	statsToMetrics(ectx.Metrics(), s)

	if impact.Enabled() {
		go impact.Enqueue(ctx, ectx, result)
	}
	return result, nil
}
