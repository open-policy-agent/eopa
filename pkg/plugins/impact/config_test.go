package impact_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/styrainc/load-private/pkg/plugins/impact"
)

func TestValidate(t *testing.T) {
	opt := cmpopts.IgnoreUnexported(impact.Config{})
	diff := func(x, y any) string {
		return cmp.Diff(x, y, opt)
	}
	isConfig := func(t *testing.T, exp impact.Config) func(*testing.T, any, error) {
		return func(t *testing.T, c any, err error) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			act, ok := c.(impact.Config)
			if !ok {
				t.Fatalf("expected %T, got %T", act, c)
			}
			if diff := diff(exp, act); diff != "" {
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
			note: "valid",
			config: `
sampling_rate: 0.1
bundle_path: testdata/load-bundle.tar.gz
publish_equal: true
`,
			checks: isConfig(t, impact.Config{
				Rate:          0.1,
				BundlePath:    "testdata/load-bundle.tar.gz",
				PublishEquals: true,
			}),
		},
		{
			note: "sample rate invalid",
			config: `
sampling_rate: 100
bundle_path: testdata/load-bundle.tar.gz 
`,
			checks: func(t *testing.T, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "sampling rate 100.000000 invalid: must be between 0 and 1 (inclusive)", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "bundle_path missing",
			config: `
sampling_rate: 0.1
`,
			checks: func(t *testing.T, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "bundle_path required", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "bunde_path: file not found",
			config: `
sampling_rate: 0.1
bundle_path: what/ever
`,
			checks: func(t *testing.T, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "open what/ever: no such file or directory", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			mgr := getTestManager()
			data, err := impact.Factory().Validate(mgr, []byte(tc.config))
			if tc.checks != nil {
				tc.checks(t, data, err)
			}
		})
	}
}
