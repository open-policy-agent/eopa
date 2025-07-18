//go:build use_opa_fork

package bundle

import (
	bundleApi "github.com/open-policy-agent/opa/v1/bundle"
)

func RegisterActivator() {
	bundleApi.RegisterActivator("_enterprise_opa", &CustomActivator{})
}
