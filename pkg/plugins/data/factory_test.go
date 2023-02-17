package data_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/load-private/pkg/plugins/data"
	"github.com/styrainc/load-private/pkg/plugins/data/kafka"
	inmem "github.com/styrainc/load-private/pkg/store"
)

func TestValidate(t *testing.T) {
	opt := cmpopts.IgnoreUnexported(kafka.Config{})
	diff := func(x, y any) string {
		return cmp.Diff(x, y, opt)
	}
	isConfig := func(t *testing.T, path string, exp kafka.Config) func(*testing.T, any, error) {
		return func(t *testing.T, c any, err error) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			cfg := c.(data.Config)
			k, ok := cfg.DataPlugins[path]
			if !ok {
				t.Fatalf("expected config under %q", path)
			}
			act, ok := k.Config.(kafka.Config)
			if !ok {
				t.Fatalf("expected %T, got %T", act, k)
			}
			if diff := diff(exp, act); diff != "" {
				t.Errorf("kafka.Config mismatch (-want +got):\n%s", diff)
			}
		}
	}
	tests := []struct {
		note   string
		config string
		checks func(*testing.T, any, error)
	}{
		{
			note: "one kafka",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
`,
			checks: isConfig(t, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				Path:              "kafka.updates",
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
			}),
		},
		{
			note: "one kafka, tls",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  tls_client_cert: kafka/testdata/tls/client-cert.pem
  tls_client_private_key: kafka/testdata/tls/client-key.pem
  tls_ca_cert: kafka/testdata/tls/ca.pem
`,
			checks: isConfig(t, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				Path:              "kafka.updates",
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				Cert:              "kafka/testdata/tls/client-cert.pem",
				PrivateKey:        "kafka/testdata/tls/client-key.pem",
				CACert:            "kafka/testdata/tls/ca.pem",
			}),
		},
		{
			note: "one kafka, sasl/plain",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  sasl_mechanism: PLAIN
  sasl_username: alice
  sasl_password: password
`,
			checks: isConfig(t, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				Path:              "kafka.updates",
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				SASLMechanism:     "PLAIN",
				SASLUsername:      "alice",
				SASLPassword:      "password",
			}),
		},
		{
			note: "one kafka, sasl/scram-sha-512",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  sasl_mechanism: SCRAM-SHA-512
  sasl_username: alice
  sasl_password: password
  sasl_token: true
`,
			checks: isConfig(t, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				Path:              "kafka.updates",
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				SASLMechanism:     "SCRAM-SHA-512",
				SASLUsername:      "alice",
				SASLPassword:      "password",
				SASLToken:         true,
			}),
		},
		{
			note: "one kafka, sasl/scram-sha-256",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  sasl_mechanism: SCRAM-SHA-256
  sasl_username: alice
  sasl_password: password
  sasl_token: true
`,
			checks: isConfig(t, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				Path:              "kafka.updates",
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				SASLMechanism:     "SCRAM-SHA-256",
				SASLUsername:      "alice",
				SASLPassword:      "password",
				SASLToken:         true,
			}),
		},
		{
			note: "one kafka, tls+sasl/scram-sha-256, lowercase",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  tls_client_cert: kafka/testdata/tls/client-cert.pem
  tls_client_private_key: kafka/testdata/tls/client-key.pem
  tls_ca_cert: kafka/testdata/tls/ca.pem
  sasl_mechanism: scram-sha-256
  sasl_username: alice
  sasl_password: password
  sasl_token: true
`,
			checks: isConfig(t, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				Path:              "kafka.updates",
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				Cert:              "kafka/testdata/tls/client-cert.pem",
				PrivateKey:        "kafka/testdata/tls/client-key.pem",
				CACert:            "kafka/testdata/tls/ca.pem",
				SASLMechanism:     "scram-sha-256",
				SASLUsername:      "alice",
				SASLPassword:      "password",
				SASLToken:         true,
			}),
		},
		{
			note: "two kafka",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
kafka.downdates:
  type: kafka
  urls:
  - some.other:8083
  topics:
  - downdates.huh
  rego_transform: data.utils.transform_events
`,
			checks: func(t *testing.T, c any, err error) {
				isConfig(t, "kafka.updates", kafka.Config{
					Topics:            []string{"updates"},
					Path:              "kafka.updates",
					URLs:              []string{"127.0.0.1:8083"},
					RegoTransformRule: "data.utils.transform_events",
				})(t, c, err)
				isConfig(t, "kafka.downdates", kafka.Config{
					Topics:            []string{"downdates.huh"},
					Path:              "kafka.downdates",
					URLs:              []string{"some.other:8083"},
					RegoTransformRule: "data.utils.transform_events",
				})(t, c, err)
			},
		},
		{
			note: "kafka, no urls",
			config: `
kafka.updates:
  type: kafka
  topics:
  - updates
`,
			checks: func(t *testing.T, _ any, err error) {
				if exp, act := "data plugin kafka (kafka.updates): need at least one broker URL", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "kafka, no topics",
			config: `
kafka.updates:
  type: kafka
  urls: ["127.0.0.1:9092"]
  topics:
`,
			checks: func(t *testing.T, _ any, err error) {
				if exp, act := "data plugin kafka (kafka.updates): need at least one topic", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "kafka, sasl, unknown mechanism",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  sasl_mechanism: SHALALALA
  sasl_username: alice
  sasl_password: password
  sasl_token: true
`,
			checks: func(t *testing.T, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "data plugin kafka (kafka.updates): unknown SASL mechanism", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			mgr := getTestManager()
			data, err := data.Factory().Validate(mgr, []byte(tc.config))
			if tc.checks != nil {
				tc.checks(t, data, err)
			}
		})
	}
}

func TestStop(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config string
	}{
		{
			name: "kafka",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
`,
		},
		{
			name: "http",
			config: `
http.test:
  type: http
  url: https://www.example.com
`,
		},
		{
			name: "okta",
			config: `
okta.test:
  type: okta
  url: https://example.com
  client_id: test
  client_secret: secret
  users: true
`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)

			mgr := getTestManager()
			c, err := data.Factory().Validate(mgr, []byte(tt.config))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			dp := data.Factory().New(mgr, c)
			ctx := context.Background()
			if err := dp.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}

			// NOTE(sr): The more time we give the go routines to actually start,
			// the less flaky this test will be, if there are leaked routines.
			time.Sleep(200 * time.Millisecond)
			dp.Stop(ctx)

			// goleak will assert that no goroutine is still running
		})
	}
}

func getTestManager() *plugins.Manager {
	return getTestManagerWithOpts(nil)
}

func getTestManagerWithOpts(config []byte, stores ...storage.Store) *plugins.Manager {
	store := inmem.New()
	if len(stores) == 1 {
		store = stores[0]
	}

	manager, err := plugins.New(config, "test-instance-id", store)
	if err != nil {
		panic(err)
	}
	return manager
}
