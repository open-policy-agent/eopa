package decisionlogs

import (
	"context"

	"github.com/benthosdev/benthos/v4/public/service"

	"github.com/open-policy-agent/opa/rego"
)

type masker interface {
	Mask(context.Context, map[string]any) (map[string]any, error)
}

type MaskProcessor struct {
	masker masker
}

func (r *MaskProcessor) Process(ctx context.Context, m *service.Message) (service.MessageBatch, error) {
	msg, err := m.AsStructured()
	if err != nil {
		return nil, err
	}

	res, err := r.masker.Mask(ctx, msg.(map[string]any))
	if err != nil {
		return nil, err
	}
	m.SetStructuredMut(res)
	return []*service.Message{m}, nil
}

func (*MaskProcessor) Close(context.Context) error {
	return nil
}

func (p *Logger) Mask(ctx context.Context, ev map[string]any) (map[string]any, error) {
	if p.maskPrep == nil {
		return ev, nil
	}
	rs, err := p.maskPrep.Eval(ctx, rego.EvalInput(ev))
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return ev, nil
	}
	return maskEvent(rs[0].Expressions[0].Value, ev)
}
