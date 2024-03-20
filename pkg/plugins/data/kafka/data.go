package kafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/types"
)

const Name = "kafka"

// Data plugin
type Data struct {
	manager        *plugins.Manager
	log            logging.Logger
	Config         Config
	client         *kgo.Client
	exit, doneExit chan struct{}

	*transform.Rego
}

// Ensure that the kafka sub-plugin will be triggered by the data umbrella plugin,
// because it implements types.Triggerer.
var _ types.Triggerer = (*Data)(nil)

func (c *Data) Start(ctx context.Context) error {
	c.exit = make(chan struct{})
	if err := c.Rego.Prepare(ctx); err != nil {
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

	if c.Config.ConsumerGroup {
		group := fmt.Sprintf("eopa_%s_%s", c.manager.ID, c.Config.Path)
		opts = append(opts, kgo.ConsumerGroup(group))
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
			if !c.Rego.Ready() { // don't fetch and drop if we're not ready
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
				before := time.Now()
				c.transformAndSave(ctx, n, rs.RecordIter())
				c.log.Debug("transformed and saved %d records in %v", n, time.Since(before))
			}
		}
	}
	close(c.doneExit)
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
	if err := c.Rego.Ingest(ctx, c.Path(), batch); err != nil {
		c.log.Error("plugin %s at %s: %w", c.Name(), c.Config.path, err)
	}
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
