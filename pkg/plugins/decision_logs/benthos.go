package decisionlogs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	_ "github.com/benthosdev/benthos/v4/public/components/aws" // aws_s3
	_ "github.com/benthosdev/benthos/v4/public/components/io"  // file/stdout cache/output
	_ "github.com/benthosdev/benthos/v4/public/components/kafka"
	_ "github.com/benthosdev/benthos/v4/public/components/otlp"
	_ "github.com/benthosdev/benthos/v4/public/components/pure"     // basics
	_ "github.com/benthosdev/benthos/v4/public/components/sql/base" // SQL internals
	_ "modernc.org/sqlite"                                          // SQLite support

	"github.com/benthosdev/benthos/v4/public/service"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-policy-agent/opa/logging"
)

type Stream interface {
	Consume(context.Context, map[string]any) error
	Run(context.Context) error
	Stop(context.Context) error
}

const tracing = "eopa_otel_global"
const drop = "dl_drop"
const mask = "dl_mask"

// NOTE(sr): These regretably have to be a global. It's OK since we only ever
// instantiate one stream. Also, we're limiting the usage to this specific point:
// NewStream will set them, the registered constructors for `dl_drop` and `dl_mask`
// will read them (defined in init(), invoked from (stream).Run).
var (
	reg       *registerer
	streamMtx sync.Mutex
)

func init() {
	service.RegisterOtelTracerProvider(tracing, service.NewConfigSpec(), func(*service.ParsedConfig) (trace.TracerProvider, error) {
		return otel.GetTracerProvider(), nil
	})

	decisionConfig := service.NewConfigSpec().
		Field(service.NewStringField("decision"))

	if err := service.RegisterProcessor(drop, decisionConfig, func(pc *service.ParsedConfig, _ *service.Resources) (service.Processor, error) {
		defer reg.wg.Done()
		return NewDrop(pc, reg)
	}); err != nil {
		panic(err)
	}
	if err := service.RegisterProcessor(mask, decisionConfig, func(pc *service.ParsedConfig, _ *service.Resources) (service.Processor, error) {
		defer reg.wg.Done()
		return NewMask(pc, reg)
	}); err != nil {
		panic(err)
	}
}

type stream struct {
	*service.Stream
	prod service.MessageHandlerFunc
}

func (s *stream) Consume(ctx context.Context, msg map[string]any) error {
	m := service.NewMessage(nil)
	m.SetStructuredMut(msg)
	return s.prod(ctx, m.WithContext(ctx))
}

func newStream(ctx context.Context, buf fmt.Stringer, out output, r0 *registerer, logger logging.Logger) (*stream, error) {
	streamMtx.Lock()
	defer streamMtx.Unlock()
	reg = r0

	st := &stream{}
	builder := service.NewStreamBuilder()
	builder.SetPrintLogger(&wrap{logger})

	var err error
	st.prod, err = builder.AddProducerFunc()
	if err != nil {
		return nil, err
	}

	if buf != nil {
		if err := builder.SetBufferYAML(buf.String()); err != nil {
			return nil, err
		}
	}

	if err := builder.AddProcessorYAML(`mapping: |
  root = @.assign(this)
  # Remove all existing metadata from messages
  meta = deleted()`); err != nil {
		return nil, err
	}
	cfg, err := json.Marshal(out.Benthos())
	if err != nil {
		return nil, err
	}
	if err := builder.AddOutputYAML(string(cfg)); err != nil {
		return nil, err
	}

	if out, ok := out.(additionalResources); ok {
		resources := out.Resources()
		if resources != nil {
			cfg, err := json.Marshal(resources)
			if err != nil {
				return nil, err
			}
			if err := builder.AddResourcesYAML(string(cfg)); err != nil {
				return nil, err
			}
		}
	}

	{ // setup tracing -- noop if global tracer is not configured
		cfg, err := json.Marshal(map[string]any{tracing: struct{}{}})
		if err != nil {
			return nil, err
		}
		if err := builder.SetTracerYAML(string(cfg)); err != nil {
			return nil, err
		}
	}

	st.Stream, err = builder.Build()
	if err != nil {
		return nil, err
	}

	go func() {
		if err := st.Run(ctx); err != nil {
			logger.Error("instantiate stream: %s", err.Error())
		}
	}()

	r0.wg.Wait() // wait for all drop/mask processors to be instantiated

	return st, nil
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
