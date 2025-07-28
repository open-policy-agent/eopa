package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/sdk"
	sdktest "github.com/open-policy-agent/opa/sdk/test"

	eopa_sdk "github.com/open-policy-agent/eopa/pkg/sdk"
)

func main() {
	ctx := context.Background()

	// create a mock HTTP bundle server
	server, err := sdktest.NewServer(sdktest.MockBundle("/bundles/bundle.tar.gz", map[string]string{
		"example.rego": `
				package authz

				import future.keywords.if

				default allow := false

				allow if input.open == "sesame"
			`,
	}))
	if err != nil {
		// handle error.
	}

	defer server.Stop()

	// provide the OPA configuration which specifies
	// fetching policy bundles from the mock server
	// and logging decisions locally to the console
	config := []byte(fmt.Sprintf(`{
		"services": {
			"test": {
				"url": %q
			}
		},
		"bundles": {
			"test": {
				"resource": "/bundles/bundle.tar.gz"
			}
		},
		"decision_logs": {
			"console": true
		}
	}`, server.URL()))

	opts := eopa_sdk.DefaultOptions()
	opts.ID = "eopa-test-1"
	opts.Config = bytes.NewReader(config)

	// create an instance of the OPA object
	opa, err := sdk.New(ctx, opts)
	if err != nil {
		// handle error.
	}

	defer opa.Stop(ctx)

	// get the named policy decision for the specified input
	if result, err := opa.Decision(ctx, sdk.DecisionOptions{Path: "/authz/allow", Input: map[string]interface{}{"open": "sesame"}}); err != nil {
		// handle error.
	} else if decision, ok := result.Result.(bool); !ok || !decision {
		// handle error.
	}
}
