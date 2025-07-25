package bundle

import (
	bundleApi "github.com/open-policy-agent/opa/v1/bundle"
)

// Ensure our custom bundle activator is available, then set it to be the
// default for all bundle activations.
func RegisterActivator() {
	bundleApi.RegisterActivator("_enterprise_opa", &CustomActivator{})
	bundleApi.RegisterDefaultBundleActivator("_enterprise_opa")
}
