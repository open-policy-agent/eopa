package kafka

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"

	"github.com/styrainc/load-private/pkg/plugins/data/types"
	inmem "github.com/styrainc/load-private/pkg/storage"
)

const Name = "kafka"

// Data plugin
type Data struct {
	manager        *plugins.Manager
	log            logging.Logger
	Config         Config
	client         *kgo.Client
	exit, doneExit chan struct{}

	transformRule ast.Ref
	transform     atomic.Pointer[rego.PreparedEvalQuery]
}

// Ensure that the kafka sub-plugin will be triggered by the data umbrella plugin,
// because it implements types.Triggerer.
var _ types.Triggerer = (*Data)(nil)

func (c *Data) Start(ctx context.Context) error {
	c.exit = make(chan struct{})
	if err := c.prepareTransform(ctx); err != nil {
		return fmt.Errorf("prepare rego_transform: %w", err)
	}
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		return storage.MakeDir(ctx, c.manager.Store, txn, c.Config.path)
	}); err != nil {
		return err
	}

	opts := []kgo.Opt{
		kgo.ConsumeTopics(c.Config.Topics...),
		kgo.SeedBrokers(c.Config.URLs...),
		kgo.WithLogger(c.kgoLogger()),
		kgo.DialTLSConfig(c.Config.tls), // if it's nil, it stays nil
	}

	switch c.Config.From {
	case "":
	case "start":
		opts = append(opts, kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	case "end":
		opts = append(opts, kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	default:
		duration, _ := time.ParseDuration(c.Config.From)
		opts = append(opts, kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-duration).UnixMilli())))
	}

	if c.Config.sasl != nil {
		opts = append(opts, kgo.SASL(c.Config.sasl))
	}
	var err error
	c.client, err = kgo.NewClient(opts...)
	if err != nil {
		return err
	}

	c.doneExit = make(chan struct{})
	go c.loop(ctx) // Q: Does this context ever stop?
	return nil
}

func (c *Data) Stop(ctx context.Context) {
	if c.doneExit == nil {
		return
	}
	c.client.Close()
	close(c.exit) // stops our polling loop
	select {
	case <-c.doneExit: // waits for polling loop to be stopped
	case <-ctx.Done(): // or exit if context canceled or timed out
	}
}

func (c *Data) Reconfigure(ctx context.Context, next any) {
	if c.Config.Equal(next.(Config)) {
		return // nothing to do
	}
	if c.doneExit != nil { // started before
		c.Stop(ctx)
	}
	c.Config = next.(Config)
	c.Start(ctx)
}

// dataPlugin accessors
func (c *Data) Name() string {
	return Name
}

func (c *Data) Path() storage.Path {
	return c.Config.path
}

func (c *Data) loop(ctx context.Context) {
LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case <-c.exit:
			break LOOP
		default:
			if !c.ready() { // don't fetch and drop if we're not ready
				time.Sleep(100 * time.Millisecond)
				continue
			}
			pollCtx, done := context.WithTimeout(ctx, 100*time.Millisecond)
			rs := c.client.PollFetches(pollCtx)
			done()

			var merr []error
			for _, err := range rs.Errors() {
				if errors.Is(err.Err, context.DeadlineExceeded) {
					continue
				}
				merr = append(merr, fmt.Errorf("fetch topic %q: %w", err.Topic, err.Err))
			}
			if merr != nil {
				c.log.Warn("error fetching records: %v", merr)
				continue
			}
			n := rs.NumRecords()
			if n > 0 {
				c.log.Debug("fetched %d records", n)
				c.transformAndSave(ctx, n, rs.RecordIter())
			}
		}
	}
	close(c.doneExit)
}

func (c *Data) ready() bool {
	return c.transform.Load() != nil
}

func mapFromRecord(r *kgo.Record) any {
	return map[string]any{
		"key":       r.Key,
		"value":     r.Value,
		"headers":   headersToSlice(r.Headers),
		"topic":     r.Topic,
		"timestamp": r.Timestamp.Unix(),
	}
}

func headersToSlice(hdrs []kgo.RecordHeader) []map[string]any {
	m := make([]map[string]any, len(hdrs))
	for i := range hdrs {
		m[i] = map[string]any{
			"key":   hdrs[i].Key,
			"value": hdrs[i].Value,
		}
	}
	return m
}

// save saves the entire batch in one go to the store
func (c *Data) transformAndSave(ctx context.Context, n int, iter *kgo.FetchesRecordIter) {
	batch := make([]any, n)
	i := 0
	for !iter.Done() {
		record := iter.Next()
		batch[i] = mapFromRecord(record)
		i++
	}
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		var results []processed
		printOut := bytes.Buffer{}
		for i := range batch {
			res, err := c.transformOne(ctx, txn, batch[i], &printOut)
			if err != nil {
				return err
			}
			if printOut.Len() > 0 {
				c.log.Debug("printOut(): %s", printOut.String())
				printOut.Reset()
			}
			if res != nil {
				results = append(results, res...)
			}
		}
		for i := range results {
			op, path, value := results[i].op, results[i].path, results[i].value
			if err := c.manager.Store.(inmem.WriterUnchecked).WriteUnchecked(ctx, txn, op, path, value); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		c.log.Error("write batch %v to %v: %v", batch, c.Config.path, err)
	}
}

func (c *Data) prepareTransform(ctx context.Context) error {
	return storage.Txn(ctx, c.manager.Store, storage.TransactionParams{}, func(txn storage.Transaction) error {
		return c.Trigger(ctx, txn)
	})
}

func (c *Data) Trigger(ctx context.Context, txn storage.Transaction) error {
	transformRef := ast.MustParseRef(c.Config.RegoTransformRule)
	query := ast.NewBody(
		ast.NewExpr(ast.NewTerm(transformRef.Append(
			ast.NewTerm(ast.NewObject(
				ast.Item(ast.StringTerm("op"), ast.VarTerm("op")),
				ast.Item(ast.StringTerm("value"), ast.VarTerm("value")),
				ast.Item(ast.StringTerm("path"), ast.VarTerm("path")),
			)),
		))),
	)

	comp := c.manager.GetCompiler()
	if comp == nil || comp.RuleTree == nil || comp.RuleTree.Find(transformRef) == nil {
		c.manager.Logger().Warn("kafka plugin (path %s): transform rule %q does not exist yet", c.Path(), transformRef)
		return nil
	}

	buf := bytes.Buffer{}
	r := rego.New(
		rego.ParsedQuery(query),
		rego.Compiler(comp),
		rego.Store(c.manager.Store),
		rego.Transaction(txn),
		rego.Runtime(c.manager.Info),
		rego.EnablePrintStatements(c.manager.EnablePrintStatements()),
		rego.PrintHook(topdown.NewPrintHook(&buf)),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return err
	}

	if buf.Len() > 0 {
		c.log.Debug("prepare print(): %s", buf.String())
	}
	c.transform.Store(&pq)
	return nil
}

type processed struct {
	op    storage.PatchOp
	path  storage.Path
	value any
}

func (c *Data) transformOne(ctx context.Context, txn storage.Transaction, message any, buf io.Writer) ([]processed, error) {
	rs, err := c.transform.Load().Eval(ctx,
		rego.EvalInput(message),
		rego.EvalTransaction(txn),
		rego.EvalPrintHook(topdown.NewPrintHook(buf)),
	)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		c.log.Debug("message discarded by transform: %v", message)
		return nil, nil
	}
	proc := make([]processed, len(rs))
	for i := range rs {
		p0, ok := rs[i].Bindings["path"]
		if !ok {
			return nil, fmt.Errorf("no path in transform bindings %v", rs[i].Bindings)
		}
		p, ok := p0.(string)
		if !ok {
			return nil, fmt.Errorf("failed to parse path %q", rs[i].Bindings["path"])
		}
		path := c.Config.path[:]
		for _, piece := range strings.Split(p, "/") {
			path = append(path, piece)
		}
		var op storage.PatchOp
		switch rs[i].Bindings["op"] {
		case "replace":
			op = storage.ReplaceOp
		case "add":
			op = storage.AddOp
		case "remove":
			op = storage.RemoveOp
		}

		proc[i] = processed{
			op:    op,
			path:  path,
			value: rs[i].Bindings["value"], // nil if not bound
		}
	}
	return proc, nil
}

func (c *Data) kgoLogger() kgo.Logger {
	return &wrap{c.log}
}

type wrap struct {
	logging.Logger
}

func (w *wrap) Level() kgo.LogLevel {
	switch w.GetLevel() {
	case logging.Error:
		return kgo.LogLevelError
	case logging.Warn:
		return kgo.LogLevelWarn
	case logging.Info:
		return kgo.LogLevelWarn
	case logging.Debug:
		return kgo.LogLevelDebug
	default:
		return kgo.LogLevelNone
	}
}

func (w *wrap) Log(level kgo.LogLevel, msg string, keyvals ...any) {
	fields := make(map[string]any, (len(keyvals)/2)+1)
	for i := 0; i < len(keyvals)/2; i++ {
		fields[keyvals[2*i].(string)] = keyvals[(2*i)+1]
	}
	fields["source"] = "data/kafka"
	switch level {
	case kgo.LogLevelError:
		w.WithFields(fields).Error(msg)
	case kgo.LogLevelWarn:
		w.WithFields(fields).Warn(msg)
	case kgo.LogLevelInfo:
		w.WithFields(fields).Info(msg)
	case kgo.LogLevelDebug:
		w.WithFields(fields).Debug(msg)
	}
}
