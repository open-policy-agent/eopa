package decisionlogs

import (
	"context"

	"github.com/benthosdev/benthos/v4/public/service"

	"github.com/open-policy-agent/opa/rego"
)

type dropper interface {
	Drop(context.Context, any) (bool, error)
}

type DropProcessor struct {
	dropper dropper
}

func (r *DropProcessor) Process(ctx context.Context, m *service.Message) (service.MessageBatch, error) {
	msg, err := m.AsStructured()
	if err != nil {
		return nil, err
	}
	drop, err := r.dropper.Drop(ctx, msg)
	switch {
	case err != nil:
		return nil, err
	case drop:
		return nil, nil
	default:
		return []*service.Message{m}, nil
	}
}

func (*DropProcessor) Close(context.Context) error {
	return nil
}

func (p *Logger) Drop(ctx context.Context, ev any) (bool, error) {
	if p.dropPrep == nil {
		return false, nil
	}
	rs, err := p.dropPrep.Eval(ctx, rego.EvalInput(ev)) // TODO(sr): rego.EvalTransaction(txn)?
	return rs.Allowed(), err
}
