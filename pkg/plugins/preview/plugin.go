package preview

import (
	"context"
	"fmt"
	"net/http"

	"github.com/open-policy-agent/opa/plugins"
)

// Plugin holds the state of  the preview plugin instantiation
type Plugin struct {
	manager *plugins.Manager
	active  bool
}

// Lookup finds the preview plugin in a plugin manager struct.
func Lookup(manager *plugins.Manager) *Plugin {
	if p := manager.Plugin(Name); p != nil {
		return p.(*Plugin)
	}
	return nil
}

// Start registers the preview handlers with the OPA HTTP REST API and sets
// the plugin status to OK.
func (p *Plugin) Start(context.Context) error {
	p.active = true
	p.manager.GetRouter().Handle(httpPrefix, p).Methods(http.MethodPost)
	p.manager.GetRouter().Handle(fmt.Sprintf("%s/{path:.+}", httpPrefix), p).Methods(http.MethodPost)

	p.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateOK})
	return nil
}

// Stop sets the plugin status to Not Ready
func (p *Plugin) Stop(context.Context) {
	p.active = false
	p.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
}

// Reconfigure is unused for the PreviewPlugin
func (*Plugin) Reconfigure(context.Context, any) {
	// noop
}
