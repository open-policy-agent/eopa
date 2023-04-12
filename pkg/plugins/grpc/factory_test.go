package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/load-private/pkg/plugins/grpc"
	inmem "github.com/styrainc/load-private/pkg/store"
)

func TestValidate(t *testing.T) {
	isConfig := func(t *testing.T, path string, exp grpc.Config) func(*testing.T, any, error) {
		return func(t *testing.T, c any, err error) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			act, ok := c.(grpc.Config)
			if !ok {
				t.Fatalf("could not convert to grpc.Confg: %v", c)
			}
			// We have to index down to the specific struct that needs the
			// ignore, otherwise cmp.Diff breaks horribly:
			if diff := cmp.Diff(exp, act, cmpopts.IgnoreUnexported(grpc.Config{}.TLS)); diff != "" {
				t.Errorf("grpc.Config mismatch (-want +got):\n%s", diff)
			}
		}
	}
	tests := []struct {
		note   string
		config string
		checks func(*testing.T, any, error)
	}{
		{
			note: "single grpc server",
			config: `
addr: 127.0.0.1:8083
`,
			checks: isConfig(t, "grpc.updates", grpc.Config{
				Addr: "127.0.0.1:8083",
			}),
		},
		{
			note:   "grpc, no address",
			config: ``,
			checks: func(t *testing.T, _ any, err error) {
				if exp, act := "need at least one address to serve from", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			mgr := getTestManager()
			g, err := grpc.Factory().Validate(mgr, []byte(tc.config))
			if tc.checks != nil {
				tc.checks(t, g, err)
			}
			ctx := context.Background()
			t.Cleanup(func() {
				mgr.Stop(ctx)
			})
		})
	}
}

func TestStop(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config string
	}{
		{
			name: "grpc basic test",
			config: `
addr: 127.0.0.1:9090
`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			ctx := context.Background()

			mgr := getTestManager()
			c, err := grpc.Factory().Validate(mgr, []byte(tt.config))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			t.Cleanup(func() { mgr.Stop(ctx) }) // Ensure manager shuts down.
			gp := grpc.Factory().New(mgr, c)
			if err := gp.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}

			// NOTE(sr): The more time we give the go routines to actually start,
			// the less flaky this test will be, if there are leaked routines.
			time.Sleep(200 * time.Millisecond)
			gp.Stop(ctx)
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
