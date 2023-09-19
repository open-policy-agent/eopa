package preview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/gorilla/mux"
	"github.com/open-policy-agent/opa/config"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	eopaStorage "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

func TestConfig(t *testing.T) {
	testCases := []struct {
		name     string
		config   string
		code     int
		response map[string]any
	}{
		{
			name:     "no configuration defaults to enabled",
			code:     http.StatusOK,
			response: map[string]any{"result": map[string]any{"a": "hello world"}},
		},
		{
			name:     "missing configuration falls back to default: enabled)",
			config:   `{}`,
			code:     http.StatusOK,
			response: map[string]any{"result": map[string]any{"a": "hello world"}},
		},
		{
			name:     "enabled: successful request",
			config:   `{"enabled":true}`,
			code:     http.StatusOK,
			response: map[string]any{"result": map[string]any{"a": "hello world"}},
		},
		{
			name:     "disabled: not found",
			config:   `{"enabled":false}`,
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
			)
			err := manager.Start(ctx)
			if err != nil {
				t.Fatalf("Unable to start plugin manager: %v", err)
			}

			hook := NewHook()
			hook.Init(manager)
			if tc.config != "" {
				hook.OnConfig(ctx, &config.Config{
					Extra: map[string]json.RawMessage{
						"preview": []byte(tc.config),
					},
				})
			}

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", nil)
			manager.GetRouter().ServeHTTP(w, r)

			if w.Code != tc.code {
				t.Fatalf("expected http status %d but received %d", tc.code, w.Code)
			}

			if tc.response != nil {
				var value map[string]any
				err = json.NewDecoder(w.Body).Decode(&value)
				if err != nil {
					t.Fatalf("could not decode response body: %v", err)
				}

				if diff := cmp.Diff(tc.response, value); diff != "" {
					t.Errorf("unexpected response body (-want, +got):\n%s", diff)
				}
			}

		})
	}
}

func TestReconfigure(t *testing.T) {
	successfulResponse := map[string]any{"result": map[string]any{"a": "hello world"}}
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
	)
	router := manager.GetRouter()
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Unable to start plugin manager: %v", err)
	}

	hook := NewHook()
	hook.Init(manager)
	hook.OnConfig(ctx, &config.Config{
		Extra: map[string]json.RawMessage{
			"preview": []byte(`{"enabled": true}`),
		},
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected http status %d but received %d", http.StatusOK, w.Code)
	}

	var value map[string]any
	err = json.NewDecoder(w.Body).Decode(&value)
	if err != nil {
		t.Fatalf("could not decode response body: %v", err)
	}

	if diff := cmp.Diff(successfulResponse, value); diff != "" {
		t.Errorf("unexpected response body (-want, +got):\n%s", diff)
	}

	// reconfigure the plugin to disable preview
	hook.OnConfigDiscovery(ctx, &config.Config{
		Extra: map[string]json.RawMessage{
			"preview": []byte(`{"enabled": false}`),
		},
	})

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/v0/preview/test", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected http status %d but received %d", http.StatusNotFound, w.Code)
	}
}

func pluginMgr(ctx context.Context, t *testing.T, seedPolicies map[string]string, seedData bjson.Json) *plugins.Manager {
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
	mgr, err := plugins.New([]byte("{}"), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}
