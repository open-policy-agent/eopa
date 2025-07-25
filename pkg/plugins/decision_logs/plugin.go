package decisionlogs

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/plugins/logs"
	"github.com/open-policy-agent/opa/v1/storage"
)

const DLPluginName = "eopa_dl" // OPA DL plugin

type Logger struct {
	manager   *plugins.Manager
	mtx       sync.Mutex
	config    Config
	stream    *stream
	callbacks []trigger
}

var _ logs.Logger = (*Logger)(nil)

type trigger func(storage.Transaction) error

type registerer struct {
	mgr      *plugins.Manager
	register func(trigger)
	wg       sync.WaitGroup
}

func (p *Logger) Start(ctx context.Context) error {
	if logs.Lookup(p.manager) == nil {
		return ErrNoDefaultPlugin
	}

	var err error
	var buffer fmt.Stringer
	switch {
	case p.config.diskBuffer != nil:
		buffer = p.config.diskBuffer
	case p.config.memoryBuffer != nil:
		buffer = p.config.memoryBuffer
	}

	// clear out callbacks
	p.callbacks = nil
	reg := registerer{
		mgr: p.manager,
		register: func(t trigger) {
			if err := storage.Txn(ctx, p.manager.Store, storage.TransactionParams{}, func(txn storage.Transaction) error {
				return t(txn)
			}); err != nil {
				p.manager.Logger().Error("compiler trigger: %v", err)
			}
			p.callbacks = append(p.callbacks, t)
		},
	}

	// NOTE(sr): We count the number of to-be-instantiated Mask and Drop processors.
	// That way, we can block in newStream until they have all been instantiated, to
	// avoid a data race.
	for _, out := range p.config.outputs {
		if out, ok := out.(interface{ NumOutputProcessors() int }); ok {
			reg.wg.Add(out.NumOutputProcessors())
		}
	}

	p.manager.RegisterCompilerTrigger(p.compilerTrigger)

	p.stream, err = newStream(ctx, buffer, p.config.outputs, &reg, p.manager.Logger())
	if err != nil {
		return err
	}

	p.manager.UpdatePluginStatus(DLPluginName, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (p *Logger) compilerTrigger(txn storage.Transaction) {
	for _, cb := range p.callbacks {
		if err := cb(txn); err != nil {
			p.manager.Logger().Error("compiler trigger: %v", err)
		}
	}
}

func (p *Logger) Log(ctx context.Context, e logs.EventV1) error {
	// Labels comes out as map[string]string, but benthos is expecting map[string]any. It seems to
	// generally work with map[string]string, but any benthos processing (specifically pulling out
	// values into meta with bloblang) is unstable and will give null values even when values
	// exist within the labels object. This converts it to map[string]any which stabilizes the
	// benthos behavior.
	labels := make(map[string]any, len(e.Labels))
	for k, v := range e.Labels {
		labels[k] = v
	}

	ev := map[string]any{
		"labels":      labels,
		"decision_id": e.DecisionID,
	}
	for k, v := range map[string]string{
		"trace_id":     e.TraceID,
		"span_id":      e.SpanID,
		"revision":     e.Revision,
		"path":         e.Path,
		"query":        e.Query,
		"requested_by": e.RequestedBy,
	} {
		if v != "" {
			ev[k] = v
		}
	}
	for k, v := range map[string]*any{
		"input":            e.Input,
		"result":           e.Result,
		"mapped_result":    e.MappedResult,
		"nd_builtin_cache": e.NDBuiltinCache,
	} {
		if v != nil {
			ev[k] = *v
		}
	}
	if e.RequestID != 0 {
		ev["req_id"] = e.RequestID
	}
	if !e.Timestamp.IsZero() {
		ev["timestamp"] = e.Timestamp
	}
	if len(e.Erased) > 0 {
		ev["erased"] = e.Erased
	}
	if len(e.Masked) > 0 {
		ev["masked"] = e.Masked
	}
	if len(e.Bundles) > 0 {
		ev["bundles"] = e.Bundles
	}
	if len(e.Metrics) > 0 {
		ev["metrics"] = e.Metrics
	}
	if e.Error != nil {
		ev["error"] = e.Error
	}
	if cc := e.Custom; len(cc) > 0 {
		ev["custom"] = cc
	}
	if ir := e.IntermediateResults; ir != nil {
		ev["intermediate_results"] = ir
	}
	return p.stream.Consume(ctx, ev)
}

func (p *Logger) Stop(ctx context.Context) {
	p.stream.Stop(ctx)
	p.manager.UpdatePluginStatus(DLPluginName, &plugins.Status{State: plugins.StateNotReady})
}

func (p *Logger) Reconfigure(ctx context.Context, config interface{}) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.manager.UpdatePluginStatus(DLPluginName, &plugins.Status{State: plugins.StateNotReady})
	p.config = config.(Config)

	p.Stop(ctx)
	if err := p.Start(ctx); err != nil {
		p.manager.UpdatePluginStatus(DLPluginName, &plugins.Status{State: plugins.StateErr})
		p.manager.Logger().Error("Reconfigure: %v", err)
	}
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
