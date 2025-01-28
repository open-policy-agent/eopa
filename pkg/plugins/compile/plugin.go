package compile

import (
	"context"

	"github.com/open-policy-agent/opa/v1/plugins"

	exp_compile "github.com/styrainc/enterprise-opa-private/pkg/compile"
)

const PluginName = "exp_compile_api"

type factory struct{}

func Factory() plugins.Factory { return &factory{} }

func (factory) New(m *plugins.Manager, _ any) plugins.Plugin {
	m.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateNotReady})
	return &cp{manager: m}
}

func (factory) Validate(*plugins.Manager, []byte) (any, error) { return nil, nil }

type cp struct{ manager *plugins.Manager }

func (p *cp) Start(context.Context) error {
	hdlr := exp_compile.Handler(p.manager.Logger())
	hdlr.SetManager(p.manager)
	hdlr.SetStore(p.manager.Store)
	p.manager.GetRouter().Handle("/exp/compile", hdlr)
	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (p *cp) Stop(context.Context) {
	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateNotReady})
}

func (*cp) Reconfigure(context.Context, any) {}
