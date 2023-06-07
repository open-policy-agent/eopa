package impact_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/impact"
)

func TestValidate(t *testing.T) {
	isConfig := func(t *testing.T, exp impact.Config) func(*testing.T, any, error) {
		return func(t *testing.T, c any, err error) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			act, ok := c.(impact.Config)
			if !ok {
				t.Fatalf("expected %T, got %T", act, c)
			}
			if diff := cmp.Diff(exp, act); diff != "" {
				t.Errorf("impact.Config mismatch (-want +got):\n%s", diff)
			}
		}
	}
	tests := []struct {
		note   string
		config string
		checks func(*testing.T, any, error)
	}{
		{
			note: "valid, defaults",
			config: `
{}
`,
			checks: isConfig(t, impact.Config{}),
		},
		{
			note: "decision logs enabled",
			config: `
decision_logs: true
`,
			checks: isConfig(t, impact.Config{
				DecisionLogs: true,
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			mgr := pluginMgr(t, "")
			data, err := impact.Factory().Validate(mgr, []byte(tc.config))
			if tc.checks != nil {
				tc.checks(t, data, err)
			}
		})
	}
}
