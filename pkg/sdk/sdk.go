package sdk

import (
	"github.com/open-policy-agent/opa/sdk"

	_ "github.com/styrainc/enterprise-opa-private/pkg/builtins" // Activate custom builtins.
	"github.com/styrainc/enterprise-opa-private/pkg/plugins"
	_ "github.com/styrainc/enterprise-opa-private/pkg/plugins/bundle" // Register .json extension
	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
	"github.com/styrainc/enterprise-opa-private/pkg/storage"
)

// DefaultOptions returns an sdk.Options struct initialized with the
// values required to use Load's features. Typically, you would add
// your specific config to its Config field.
func DefaultOptions() sdk.Options {
	rego_vm.SetDefault(true)

	return sdk.Options{
		Plugins: plugins.All(),
		Store:   storage.New(),
	}
}
