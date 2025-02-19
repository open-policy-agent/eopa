package sdk_test

import (
	"testing"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/styrainc/enterprise-opa-private/pkg/compile"
)

// TestCompile ensures that the compile package can be used with OSS OPA.
func TestCompile(*testing.T) {
	l := logging.New()
	_ = compile.Handler(l)
}
