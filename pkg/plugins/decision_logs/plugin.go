package decisionlogs

import (
	"context"
	"fmt"
	"sync"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/logs"
)

const DLPluginName = "eopa_dl" // OPA DL plugin

var _ logs.Logger = (*Logger)(nil)

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

	p.stream, err = NewStream(ctx, buffer, p.config.outputs, p.manager.Logger())
	if err != nil {
		return err
	}
	go p.stream.Run(ctx)

	p.manager.UpdatePluginStatus(DLPluginName, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (p *Logger) Log(ctx context.Context, e logs.EventV1) error {
	ev := map[string]any{
		"labels":      e.Labels,
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
			ev[k] = v
		}
	}
	if e.RequestID != 0 {
		ev["req_id"] = e.RequestID
	}
	if !e.Timestamp.IsZero() {
		ev["timestamp"] = e.Timestamp // TODO(sr): encoding
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

	return p.stream.Consume(ctx, ev)
}

type Logger struct {
	manager *plugins.Manager
	mtx     sync.Mutex
	config  Config
	stream  Stream
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
