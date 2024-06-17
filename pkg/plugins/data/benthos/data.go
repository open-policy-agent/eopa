package benthos

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/redpanda-data/benthos/v4/public/components/pure" // tracer "none"
	"github.com/redpanda-data/benthos/v4/public/service"
	_ "github.com/redpanda-data/connect/v4/public/components/pulsar"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/types"
)

type Data struct {
	manager *plugins.Manager
	log     logging.Logger
	Config  Config
	stream  *service.Stream
	exit    chan struct{}

	*transform.Rego
}

// Ensure that the benthos sub-plugin will be triggered by the data umbrella plugin,
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

	var benthos map[string]any
	switch inp := c.Config.Input.(type) {
	case PulsarConfig:
		benthos = map[string]any{
			"batched": map[string]any{
				"child": map[string]any{
					"pulsar": inp,
				},
				"policy": map[string]any{
					"count":  200,
					"period": "200ms",
				},
			},
		}
	default:
		panic("unreachable")
	}
	str, err := newStream(benthos, c.log, c.consumePulsar)
	if err != nil {
		return fmt.Errorf("pulsar: %w", err)
	}
	c.stream = str

	go c.stream.Run(ctx)

	return nil
}

func (c *Data) consumePulsar(ctx context.Context, mb service.MessageBatch) error {
	n := len(mb)
	before := time.Now()
	batch := make([]any, n)
	var err error
	for i := range mb {
		batch[i], err = mapFromRecord(mb[i])
		if err != nil {
			return err
		}
	}
	if err := c.Rego.Ingest(ctx, c.Path(), batch); err != nil {
		c.log.Error("plugin %s at %s: %v", c.Name(), c.Config.path, err)
	}
	c.log.WithFields(map[string]any{
		"count":       n,
		"duration_ms": time.Since(before).Milliseconds(),
	}).Debug("transformed and saved %d records in %v", n, time.Since(before))

	return nil
}

func mapFromRecord(msg *service.Message) (any, error) {
	bs, err := msg.AsBytes()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":        meta(msg, "pulsar_message_id"),
		"key":       meta(msg, "pulsar_key"),
		"topic":     meta(msg, "pulsar_topic"),
		"producer":  meta(msg, "pulsar_producer_name"),
		"timestamp": meta(msg, "pulsar_publish_time_unix"),
		"value":     bs,
	}, nil
}

func meta(msg *service.Message, key string) any {
	val, _ := msg.MetaGetMut(key)
	return val
}

func (c *Data) Stop(ctx context.Context) {
	c.stream.Stop(ctx)
}

func (c *Data) Reconfigure(ctx context.Context, next any) {
	if c.Config.Equal(next.(Config)) {
		return // nothing to do
	}
	c.Stop(ctx)
	c.Config = next.(Config)
	c.Start(ctx)
}

// dataPlugin accessors
func (c *Data) Name() string {
	switch c.Config.Input.(type) {
	case PulsarConfig:
		return string(Pulsar)
	}
	panic("unreachable")
}

func (c *Data) Path() storage.Path {
	return c.Config.path
}

func newStream(cfg map[string]any, logger logging.Logger, consumer service.MessageBatchHandlerFunc) (*service.Stream, error) {
	builder := service.NewStreamBuilder()
	builder.SetPrintLogger(&wrap{logger})

	c, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := builder.AddInputYAML(string(c)); err != nil {
		return nil, err
	}

	if err := builder.AddBatchConsumerFunc(consumer); err != nil {
		return nil, err
	}

	return builder.Build()
}

type wrap struct {
	l logging.Logger
}

func (w wrap) Println(v ...any) {
	line := strings.Builder{}
	for i := range v {
		if i != 0 {
			line.WriteString(" ")
		}
		fmt.Fprintf(&line, "%v", v[i])
	}
	w.l.Debug(line.String())
}

func (w wrap) Printf(f string, v ...any) {
	w.l.Debug(strings.TrimRight(f, "\n"), v...)
}
