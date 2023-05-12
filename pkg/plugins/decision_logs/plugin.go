package decisionlogs

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/logs"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown/builtins"

	"github.com/styrainc/load-private/pkg/vm"
)

const Name = "load_decision_logger"

type Logger struct {
	manager *plugins.Manager
	mtx     sync.Mutex
	config  Config
	stream  Stream

	dropPrep *rego.PreparedEvalQuery
	dropMtx  sync.Mutex

	maskPrep *rego.PreparedEvalQuery
	maskMtx  sync.Mutex
}

func (p *Logger) Start(ctx context.Context) error {
	if logs.Lookup(p.manager) != nil {
		return fmt.Errorf("%s cannot be used together with OPA's decision logging", Name)
	}

	var err error
	var buffer fmt.Stringer
	switch {
	case p.config.diskBuffer != nil:
		buffer = p.config.diskBuffer
	case p.config.memoryBuffer != nil:
		buffer = p.config.memoryBuffer
	}

	p.stream, err = NewStream(ctx, p, p, buffer, p.config.outputs, p.manager.Logger())
	if err != nil {
		return err
	}
	go p.stream.Run(ctx)
	mutex.Lock()
	singleton = p
	mutex.Unlock()

	if err := storage.Txn(ctx, p.manager.Store, storage.TransactionParams{}, func(txn storage.Transaction) error {
		if err := p.updateDropPrep(ctx, txn); err != nil {
			return err
		}
		return p.updateMaskPrep(ctx, txn)
	}); err != nil {
		return err
	}

	p.manager.RegisterCompilerTrigger(func(txn storage.Transaction) {
		if err := p.updateDropPrep(context.Background(), txn); err != nil {
			p.manager.Logger().Error("update drop decision: %v", err)
		}
	})
	p.manager.RegisterCompilerTrigger(func(txn storage.Transaction) {
		if err := p.updateMaskPrep(context.Background(), txn); err != nil {
			p.manager.Logger().Error("update mask decision: %v", err)
		}
	})
	p.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (p *Logger) Stop(ctx context.Context) {
	p.stream.Stop(ctx)
	p.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
}

func (p *Logger) Reconfigure(ctx context.Context, config interface{}) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
	p.config = config.(Config)

	p.Stop(ctx)
	if err := p.Start(ctx); err != nil {
		p.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateErr})
		p.manager.Logger().Error("Reconfigure: %v", err)
	}
}

func (p *Logger) updateDropPrep(ctx context.Context, txn storage.Transaction) error {
	if p.config.DropDecision == "" {
		return nil
	}
	dropRef, err := parseDataPath(p.config.DropDecision)
	if err != nil {
		return err
	}

	query := ast.NewBody(ast.NewExpr(ast.NewTerm(dropRef)))
	r := rego.New(
		rego.ParsedQuery(query),
		rego.Compiler(p.manager.GetCompiler()),
		rego.Store(p.manager.Store),
		rego.Transaction(txn),
		rego.Runtime(p.manager.Info),
		rego.EnablePrintStatements(p.manager.EnablePrintStatements()),
		rego.PrintHook(p.manager.PrintHook()),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return err
	}
	p.dropMtx.Lock()
	p.dropPrep = &pq
	p.dropMtx.Unlock()
	return nil
}

func (p *Logger) updateMaskPrep(ctx context.Context, txn storage.Transaction) error {
	if p.config.MaskDecision == "" {
		return nil
	}
	maskRef, err := parseDataPath(p.config.MaskDecision)
	if err != nil {
		return err
	}

	query := ast.NewBody(ast.NewExpr(ast.NewTerm(maskRef)))
	r := rego.New(
		rego.ParsedQuery(query),
		rego.Compiler(p.manager.GetCompiler()),
		rego.Store(p.manager.Store),
		rego.Transaction(txn),
		rego.Runtime(p.manager.Info),
		rego.EnablePrintStatements(p.manager.EnablePrintStatements()),
		rego.PrintHook(p.manager.PrintHook()),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return err
	}
	p.maskMtx.Lock()
	p.maskPrep = &pq
	p.maskMtx.Unlock()
	return nil
}

var singleton *Logger
var mutex = &sync.Mutex{}

// TODO(sr) dedup type definition (used with LIA, too)
type EvalContext interface {
	CompiledQuery() ast.Body
	NDBCache() builtins.NDBCache
	ParsedInput() ast.Value
	Metrics() metrics.Metrics
}

func Log(ctx context.Context, ectx EvalContext, result ast.Value, evalErr error) error {
	if singleton == nil {
		// TODO(sr): should we attempt to retry? maybe to avoid losing DL entries on service startup?
		// Let's verify what's going on there.
		return nil // plugin not enabled
	}

	dataOps := &vm.DataOperations{}

	decisionID := DecisionIDFromContext(ctx)
	rctx, ok := logging.FromContext(ctx)
	if !ok {
		return nil
	}

	m := ectx.Metrics()
	var in any
	if input := ectx.ParsedInput(); input != nil {
		// TODO(sr): this roundtrip is to get from `ast.Value` to `any`, but different from
		// ast.ValueToInterface -- the VM code doesn't deal well with certain types.
		var err error
		in, err = roundtrip(ctx, dataOps, input)
		if err != nil {
			return err
		}
	}

	res := unwrap(result)
	var r any
	if res != nil {
		var err error
		r, err = roundtrip(ctx, dataOps, res)
		if err != nil {
			return err
		}
	}

	m.Timer("server_handler").Stop() // close enough to what OPA does
	meta := map[string]any{
		"labels":      singleton.manager.Labels(),
		"decision_id": decisionID,
		"timestamp":   time.Now(),
		"metrics":     m.All(), // TODO(sr): default plugin thinks `m` could be nil
	}

	ev := map[string]any{ // TODO: check if it's OK to do without these
		// Bundles:        nil,
		// Query:          ectx.CompiledQuery().String(),
		// MappedResult:       nil,
	}
	if in != nil {
		ev["input"] = in
	}
	if res != nil {
		ev["result"] = r
	}
	if evalErr != nil {
		ev["error"] = evalErr.Error()
	}

	if ndbc := ectx.NDBCache(); ndbc != nil {
		m := make(map[string]any, len(ndbc))
		for k, obj := range ndbc {
			v, err := dataOps.FromInterface(ctx, obj)
			if err != nil {
				return err
			}
			v, err = dataOps.ToInterface(ctx, v)
			if err != nil {
				return err
			}
			m[k] = v
		}
		ev["nd_builtin_cache"] = m
	}

	// TODO(sr): deal with logging not having this set up
	if rctx != nil {
		meta["path"] = dropDataPrefix(rctx.ReqPath)
		meta["req_id"] = int64(rctx.ReqID)
		meta["requested_by"] = rctx.ClientAddr
	}
	// TODO(sr): add "type" to meta
	return singleton.stream.Consume(ctx, ev, meta)
}

func unwrap(result ast.Value) ast.Value {
	var resultObj ast.Value
	s := result.(ast.Set)
	if s.Len() == 0 {
		return nil
	}
	_ = s.Iter(func(t *ast.Term) error {
		resultObj = t.Value
		return nil
	})
	var res0 ast.Value
	_ = resultObj.(ast.Object).Iter(func(_, v *ast.Term) error {
		res0 = v.Value
		return nil
	})
	return res0
}

func dropDataPrefix(p string) string {
	return strings.TrimPrefix(strings.TrimPrefix(p, "/v1/data/"), "/v0/data/")
}

// TODO: validate config beforehand
// parseDataPath returns a ref from the slash separated path s rooted at data.
// All path segments are treated as identifier strings.
func parseDataPath(s string) (ast.Ref, error) {
	s = "/" + strings.TrimPrefix(s, "/")

	path, ok := storage.ParsePath(s)
	if !ok {
		return nil, fmt.Errorf("invalid path: %s", s)
	}

	return path.Ref(ast.DefaultRootDocument), nil
}

func roundtrip(ctx context.Context, dataOps *vm.DataOperations, x ast.Value) (any, error) {
	y, err := dataOps.FromInterface(ctx, x)
	if err != nil {
		return nil, err
	}
	return dataOps.ToInterface(ctx, y)
}
