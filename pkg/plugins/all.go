package plugins

import (
	"github.com/open-policy-agent/opa/plugins"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	dl "github.com/styrainc/enterprise-opa-private/pkg/plugins/decision_logs"
	// "github.com/styrainc/enterprise-opa-private/pkg/plugins/grpc"
	// "github.com/styrainc/enterprise-opa-private/pkg/plugins/impact"
)

func All() map[string]plugins.Factory {
	return map[string]plugins.Factory{
		data.Name: data.Factory(),
		// impact.Name:     impact.Factory(),
		// grpc.PluginName: grpc.Factory(),
		dl.DLPluginName: dl.Factory(),
	}
}
