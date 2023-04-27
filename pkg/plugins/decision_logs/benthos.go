package decisionlogs

import (
	"context"
	"fmt"
	"strings"

	_ "github.com/benthosdev/benthos/v4/public/components/aws" // aws_s3
	_ "github.com/benthosdev/benthos/v4/public/components/io"  // file/stdout cache/output
	_ "github.com/benthosdev/benthos/v4/public/components/kafka"
	_ "github.com/benthosdev/benthos/v4/public/components/pure" // basics
	_ "github.com/benthosdev/benthos/v4/public/components/sql"  // sqlite

	"github.com/benthosdev/benthos/v4/public/service"

	"github.com/open-policy-agent/opa/logging"
)

type Stream interface {
	Consume(context.Context, map[string]any, map[string]any) error
	Run(context.Context) error
	Stop(context.Context) error
}

type stream struct {
	*service.Stream
	prod service.MessageHandlerFunc
}

func (s *stream) Consume(ctx context.Context, msg, meta map[string]any) error {
	m := service.NewMessage(nil)
	m.SetStructuredMut(msg)
	for k := range meta {
		m.MetaSetMut(k, meta[k])
	}
	return s.prod(ctx, m)
}

func NewStream(_ context.Context, mask masker, drop dropper, buf, output fmt.Stringer, logger logging.Logger) (Stream, error) {
	if err := service.RegisterProcessor("dl_drop", service.NewConfigSpec(), func(*service.ParsedConfig, *service.Resources) (service.Processor, error) {
		return &DropProcessor{dropper: drop}, nil
	}); err != nil {
		return nil, err
	}
	if err := service.RegisterProcessor("dl_mask", service.NewConfigSpec(), func(*service.ParsedConfig, *service.Resources) (service.Processor, error) {
		return &MaskProcessor{masker: mask}, nil
	}); err != nil {
		return nil, err
	}

	builder := service.NewStreamBuilder()
	builder.SetPrintLogger(&wrap{logger})

	produce, err := builder.AddProducerFunc()
	if err != nil {
		return nil, err
	}

	if buf != nil {
		if err := builder.SetBufferYAML(buf.String()); err != nil {
			return nil, err
		}
	}
	if err := builder.AddProcessorYAML(`dl_drop: {}`); err != nil {
		return nil, err
	}
	if err := builder.AddProcessorYAML(`dl_mask: {}`); err != nil {
		return nil, err
	}
	if err := builder.AddProcessorYAML(`mapping: |
  root = @.assign(this)
  # Remove all existing metadata from messages
  meta = deleted()`); err != nil {
		return nil, err
	}
	if err := builder.AddOutputYAML(output.String()); err != nil {
		return nil, err
	}

	s, err := builder.Build()
	if err != nil {
		return nil, err
	}

	return &stream{Stream: s, prod: produce}, nil
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
