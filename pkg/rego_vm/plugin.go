// Package rego_vm contains the rego target plugin to be used with OPA.
package rego_vm

import (
	"context"
	"crypto/rand"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	_ "github.com/styrainc/enterprise-opa-private/pkg/plugins/bundle" // register bjson extension
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/impact"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

const Name = "rego_target_vm"
const Target = "vm"

var defaultTgt = false

func init() {
	rego.RegisterPlugin(Name, &vmp{})
}

var limits *vm.Limits
var limitsMtx sync.Mutex

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

func (*vmp) PrepareForEval(_ context.Context, policy *ir.Policy, _ ...rego.PrepareOption) (rego.TargetPluginEval, error) {
	executable, err := vm.NewCompiler().WithPolicy(policy).Compile()
	if err != nil {
		return nil, err
	}

	return &vme{
		vm: vm.NewVM().WithExecutable(executable),
	}, nil
}

type vme struct {
	vm *vm.VM
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
	// and feed its data to the VM. That will have sublte differences in behavior; but it
	// is good enough for the remaining cases where this is allowed to happen: discovery
	// document evaluation.
	var v *vm.VM
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
		v = t.vm.WithDataJSON(y)
	} else {
		v = t.vm.WithDataNamespace(txn)
	}

	result, err := v.Eval(ctx, "eval", vm.EvalOpts{
		Metrics:                ectx.Metrics(),
		Input:                  input,
		Time:                   ectx.Time(),
		Seed:                   seed,
		Runtime:                runtime(rt),
		Cache:                  builtins.Cache{},
		NDBCache:               ectx.NDBCache(),
		InterQueryBuiltinCache: ectx.InterQueryBuiltinCache(),
		PrintHook:              ectx.PrintHook(),
		StrictBuiltinErrors:    ectx.StrictBuiltinErrors(),
		Capabilities:           ectx.Capabilities(),
		TracingOpts:            tracingOpts(ectx),
		Limits:                 limits,
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

	go impact.Enqueue(ctx, ectx, result)
	return result, nil
}

func runtime(rt ast.Value) ast.Value {
	if rt == nil {
		return ast.NewObject()
	}
	return rt
}
