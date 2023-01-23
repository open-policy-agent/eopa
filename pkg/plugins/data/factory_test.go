package data_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/load/pkg/plugins/data"
	"github.com/styrainc/load/pkg/plugins/data/kafka"
	inmem "github.com/styrainc/load/pkg/store"
)

func TestValidate(t *testing.T) {
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
  brokerURLs:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
`,
			checks: func(t *testing.T, c any, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				cfg := c.(data.Config)
				k, ok := cfg.DataPlugins["kafka.updates"]
				if !ok {
					t.Fatalf("expected config under 'kafka.updates'")
				}
				act, ok := k.Config.(kafka.Config)
				if !ok {
					t.Fatalf("expected %T, got %T", act, k)
				}
				exp := kafka.Config{
					Topics:            []string{"updates"},
					Path:              "kafka.updates",
					BrokerURLs:        []string{"127.0.0.1:8083"},
					RegoTransformRule: "data.utils.transform_events",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("kafka.Config mismatch (-want +got):\n%s", diff)
				}
			},
		},
		{
			note: "two kafka",
			config: `
kafka.updates:
  type: kafka
  brokerURLs:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
kafka.downdates:
  type: kafka
  brokerURLs:
  - some.other:8083
  topics:
  - downdates.huh
  rego_transform: data.utils.transform_events
`,
			checks: func(t *testing.T, c any, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				cfg := c.(data.Config)
				{
					k, ok := cfg.DataPlugins["kafka.updates"]
					if !ok {
						t.Fatalf("expected config under 'kafka.updates'")
					}
					act, ok := k.Config.(kafka.Config)
					if !ok {
						t.Fatalf("expected %T, got %T", act, k)
					}
					exp := kafka.Config{
						Topics:            []string{"updates"},
						Path:              "kafka.updates",
						BrokerURLs:        []string{"127.0.0.1:8083"},
						RegoTransformRule: "data.utils.transform_events",
					}
					if diff := cmp.Diff(exp, act); diff != "" {
						t.Errorf("kafka.Config mismatch (-want +got):\n%s", diff)
					}
				}
				{
					k, ok := cfg.DataPlugins["kafka.downdates"]
					if !ok {
						t.Fatalf("expected config under 'kafka.downdates'")
					}
					act, ok := k.Config.(kafka.Config)
					if !ok {
						t.Fatalf("expected %T, got %T", act, k)
					}
					exp := kafka.Config{
						Topics:            []string{"downdates.huh"},
						Path:              "kafka.downdates",
						BrokerURLs:        []string{"some.other:8083"},
						RegoTransformRule: "data.utils.transform_events",
					}
					if diff := cmp.Diff(exp, act); diff != "" {
						t.Errorf("kafka.Config mismatch (-want +got):\n%s", diff)
					}
				}
			},
		},
		{
			note: "kafka, no brokerURLs",
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
  brokerURLs: ["127.0.0.1:9092"]
  topics:
`,
			checks: func(t *testing.T, _ any, err error) {
				if exp, act := "data plugin kafka (kafka.updates): need at least one topic", err.Error(); exp != act {
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
