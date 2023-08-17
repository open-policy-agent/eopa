package preview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/gorilla/mux"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/open-policy-agent/opa/storage"
	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	eopaStorage "github.com/styrainc/enterprise-opa-private/pkg/storage"
	"go.uber.org/goleak"
)

func TestConfig(t *testing.T) {
	testCases := []struct {
		name     string
		config   string
		code     int
		response map[string]any
	}{
		{
			name:     "successful request",
			config:   `{"plugins":{"preview":{}}}`,
			code:     http.StatusOK,
			response: map[string]any{"result": map[string]any{"a": "hello world"}},
		},
		{
			name:     "not configured",
			config:   `{}`,
			code:     http.StatusNotFound,
			response: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			manager := pluginMgr(
				ctx,
				t,
				map[string]string{
					"a.rego": "package test\n\na := data.foo.bar",
				},
				bjson.MustNew(map[string]any{
					"foo": map[string]any{
						"bar": "hello world",
					},
				}),
				tc.config,
			)
			router := manager.GetRouter()

			err := manager.Start(ctx)
			if err != nil {
				t.Fatalf("Unable to start plugin manager: %v", err)
			}

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", nil)
			router.ServeHTTP(w, r)

			if w.Code != tc.code {
				t.Fatalf("expected http status %d but received %d", tc.code, w.Code)
			}

			var value map[string]any
			json.NewDecoder(w.Body).Decode(&value)
			if diff := cmp.Diff(tc.response, value); diff != "" {
				t.Errorf("unexpected response body (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestStop(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx := context.Background()
	manager := pluginMgr(
		ctx,
		t,
		map[string]string{
			"a.rego": "package test\n\na := data.foo.bar",
		},
		bjson.MustNew(map[string]any{
			"foo": map[string]any{
				"bar": "hello world",
			},
		}),
		`{"plugins":{"preview":{}}}`,
	)
	router := manager.GetRouter()

	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Unable to start plugin manager: %v", err)
	}
	// immediately stop the plugin
	plugin := Lookup(manager)
	plugin.Stop(ctx)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected http status %d but received %d", http.StatusNotFound, w.Code)
	}

	// goleak will assert that no goroutine is still running
}

func pluginMgr(ctx context.Context, t *testing.T, seedPolicies map[string]string, seedData bjson.Json, config string) *plugins.Manager {
	t.Helper()
	mux := mux.NewRouter()
	opts := []func(*plugins.Manager){
		plugins.WithRouter(mux),
		plugins.EnablePrintStatements(true),
	}
	var store storage.Store
	if seedData != nil {
		store = eopaStorage.NewFromObject(seedData)
	} else {
		store = eopaStorage.New()
	}
	if seedPolicies != nil {
		txn, err := store.NewTransaction(ctx, storage.WriteParams)
		if err != nil {
			t.Fatalf("could not create a storage transaction: %v", err)
		}
		for key, module := range seedPolicies {
			store.UpsertPolicy(ctx, txn, key, []byte(module))
		}
		store.Commit(ctx, txn)
	}
	mgr, err := plugins.New([]byte(config), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	disco, err := discovery.New(mgr,
		discovery.Factories(map[string]plugins.Factory{
			Name: Factory(),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}
