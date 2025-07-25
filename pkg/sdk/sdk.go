package sdk

import (
	"github.com/open-policy-agent/opa/v1/hooks"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/sdk"

	"github.com/open-policy-agent/eopa/pkg/builtins"
	"github.com/open-policy-agent/eopa/pkg/ekm"
	"github.com/open-policy-agent/eopa/pkg/plugins"
	_ "github.com/open-policy-agent/eopa/pkg/plugins/bundle" // Register .json extension
	"github.com/open-policy-agent/eopa/pkg/rego_vm"
	"github.com/open-policy-agent/eopa/pkg/storage"
)

// DefaultOptions returns an sdk.Options struct initialized with the values
// required to use Enterprise OPA's features. Typically, you would add your
// specific config to its Config field.
func DefaultOptions() sdk.Options {
	rego_vm.SetDefault(true)
	builtins.Init()

	ekmHook := ekm.NewEKM()
	ekmHook.SetLogger(logging.NewNoOpLogger())

	return sdk.Options{
		Plugins: plugins.All(),
		Store:   storage.New(),
		Hooks:   hooks.New(ekmHook),
	}
}
