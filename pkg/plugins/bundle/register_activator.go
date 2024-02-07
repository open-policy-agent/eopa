//go:build use_opa_fork

package bundle

import (
	bundleApi "github.com/open-policy-agent/opa/bundle"
)

func RegisterActivator() {
	bundleApi.RegisterBundleActivator(&CustomActivator{})
}
