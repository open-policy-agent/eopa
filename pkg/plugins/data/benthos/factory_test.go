package benthos_test

import (
	"context"
	"strings"
	"testing"
	"time"

	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

func TestBenthosLint(t *testing.T) {
	t.Parallel()

	// NOTE(sr): These tests ensure that we don't spit out benthos configs
	// that are invalid.
	tests := []struct {
		note   string
		config string
	}{
		{
			note: "pulsar/minimal",
			config: `type: pulsar
url: pulsar://localhost:6650
topics:
- foobar
rego_transform: data.pulsar.transform
`,
		},
		{
			note: "pulsar/subscription",
			config: `type: pulsar
url: pulsar://localhost:6650
topics:
- foobar
rego_transform: data.pulsar.transform
subscription_type: shared
subscription_name: whatever
subscription_initial_position: latest
`,
		},
		{
			note: "pulsar/auth/token",
			config: `type: pulsar
url: pulsar://localhost:6650
topics:
- foobar
rego_transform: data.pulsar.transform
auth_token: jwtohnee
`,
		},
		{
			note: "pulsar/auth/oauth2",
			config: `type: pulsar
url: pulsar://localhost:6650
topics:
- foobar
rego_transform: data.pulsar.transform
issuer_url: https://oauth2
client_id: sesame
client_secret: street
audience: everyone
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			t.Parallel()

			config := `plugins:
  data:
    pulsar:
`
			for _, s := range strings.Split(tc.config, "\n") {
				config += strings.Repeat(" ", 6) + s + "\n"
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			pm := pluginMgr(ctx, t, inmem.New(), config)
			if err := pm.Start(ctx); err != nil {
				t.Fatal(err)
			}
			pm.Stop(ctx)
		})
	}
}
