package http_test

import (
	"context"
	gojson "encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/plugins/discovery"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/util"

	common "github.com/styrainc/enterprise-opa-private/pkg/internal/goleak"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, common.Defaults...)
}

func TestHTTPData(t *testing.T) {
	t.Parallel()

	const transform = `package e2e
	transform.users[id] := d if {
		entry := input.incoming
		id := entry.id
		d := entry.userId
	}
	`

	for _, tt := range []struct {
		name         string
		handler      func(tb testing.TB) http.HandlerFunc
		config       string
		expectedData []map[string]any
	}{
		{
			name: "simple",
			config: `
plugins:
  data:
    http.placeholder:
      type: http
      url: %[1]s
`,
			expectedData: []map[string]any{
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
				},
			},
			handler: func(testing.TB) http.HandlerFunc {
				return func(writer http.ResponseWriter, _ *http.Request) {
					writer.Write([]byte(`
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
					))
				}
			},
		},
		{
			name: "transform",
			config: `
plugins:
  data:
    http.placeholder:
      type: http
      url: %[1]s
      rego_transform: data.e2e.transform
`,
			expectedData: []map[string]any{
				{
					"users": map[string]any{
						"id01": "admin",
					},
				},
			},
			handler: func(testing.TB) http.HandlerFunc {
				return func(writer http.ResponseWriter, _ *http.Request) {
					writer.Write([]byte(`
{
  "userId": "admin",
  "id": "id01",
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
					))
				}
			},
		},
		{
			name: "two requests",
			config: `
plugins:
  data:
    http.placeholder:
      type: http
      url: %[1]s
      polling_interval: 1s
`,
			expectedData: []map[string]any{
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
				},
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "quia et suscipit suscipit recusandae consequuntur expedita et cum reprehenderit molestiae ut ut quas totam nostrum rerum est autem sunt rem eveniet architecto",
				},
			},
			handler: func(testing.TB) http.HandlerFunc {
				first := true
				return func(writer http.ResponseWriter, _ *http.Request) {
					if first {
						writer.Write([]byte(`
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit"
}`))
						first = false
					} else {
						writer.Write([]byte(`
{
  "userId": 1,
  "id": 1,
  "title": "quia et suscipit suscipit recusandae consequuntur expedita et cum reprehenderit molestiae ut ut quas totam nostrum rerum est autem sunt rem eveniet architecto"
}`))
					}
				}
			},
		},
		{
			name: "body",
			config: `
plugins:
  data:
    http.placeholder:
      type: http
      url: %[1]s
      body: Excludere im sapientia evidenter et delusisse. Externarum vi requiratur in judicarent an cavillandi.
`,
			expectedData: []map[string]any{
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
				},
			},
			handler: func(tb testing.TB) http.HandlerFunc {
				return func(writer http.ResponseWriter, request *http.Request) {
					if request.Method != http.MethodGet {
						tb.Fatalf("unexpected method %q, should be GET", request.Method)
					}
					data, err := io.ReadAll(request.Body)
					if err != nil {
						tb.Fatalf("reading request failed: %+v", err)
					}
					if string(data) != "Excludere im sapientia evidenter et delusisse. Externarum vi requiratur in judicarent an cavillandi." {
						tb.Fatalf("received unexpected data: %s", string(data))
					}

					writer.Write([]byte(`
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
					))
				}
			},
		},
		{
			name: "file",
			config: `
plugins:
  data:
    http.placeholder:
      type: http
      url: %[1]s
      file: testdata/file_request.txt
      method: POST
`,
			expectedData: []map[string]any{
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
				},
			},
			handler: func(tb testing.TB) http.HandlerFunc {
				return func(writer http.ResponseWriter, request *http.Request) {
					if request.Method != http.MethodPost {
						tb.Fatalf("unexpected method %q, should be POST", request.Method)
					}
					data, err := io.ReadAll(request.Body)
					if err != nil {
						tb.Fatalf("reading request failed: %+v", err)
					}
					if string(data) != "Excludere im sapientia evidenter et delusisse. Externarum vi requiratur in judicarent an cavillandi." {
						tb.Fatalf("received unexpected data: %s", string(data))
					}

					writer.Write([]byte(`
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
					))
				}
			},
		},
		{
			name: "custom method",
			config: `
plugins:
  data:
    http.placeholder:
      type: http
      url: %[1]s
      method: CUSTOM-TEST-METHOD
`,
			expectedData: []map[string]any{
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
				},
			},
			handler: func(tb testing.TB) http.HandlerFunc {
				return func(writer http.ResponseWriter, request *http.Request) {
					if request.Method != "CUSTOM-TEST-METHOD" {
						tb.Fatalf("unexpected method %q, should be CUSTOM-TEST-METHOD", request.Method)
					}
					writer.Write([]byte(`
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
					))
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler(t))
			defer srv.Close()
			ctx := context.Background()
			config := fmt.Sprintf(tt.config, srv.URL)

			store := storeWithPolicy(ctx, t, transform)
			mgr := pluginMgr(t, store, config)

			if err := mgr.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer mgr.Stop(ctx)

			waitForStorePath(ctx, t, store, "/http/placeholder")
			for i, exp := range tt.expectedData {
				act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/http/placeholder"))
				if err != nil {
					t.Fatalf("read back data: %v", err)
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("data value mismatch for #%d, diff:\n%s", i, diff)
				}
				// wait until next interval, except this is the last expected item
				if i != len(tt.expectedData)-1 {
					time.Sleep(1 * time.Second)
				}
			}
		})
	}
}

func TestHTTPOwned(t *testing.T) {
	t.Parallel()

	config := `
plugins:
  data:
    http.placeholder:
      type: http
      url: %[1]s
`
	var handler http.HandlerFunc = func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte(`
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
		))
	}

	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx := context.Background()
	cfg := fmt.Sprintf(config, srv.URL)

	store := inmem.New()
	mgr := pluginMgr(t, store, cfg)

	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	// test owned path
	err := storage.WriteOne(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/http/placeholder"), map[string]any{"foo": "bar"})
	if err == nil || err.Error() != `path "/http/placeholder" is owned by plugin "http"` {
		t.Errorf("owned check failed, got %v", err)
	}
}

func pluginMgr(t *testing.T, store storage.Store, config string) *plugins.Manager {
	t.Helper()
	h := topdown.NewPrintHook(os.Stderr)
	opts := []func(*plugins.Manager){
		plugins.PrintHook(h),
		plugins.EnablePrintStatements(true),
	}
	if !testing.Verbose() {
		opts = append(opts, plugins.Logger(logging.NewNoOpLogger()))
		opts = append(opts, plugins.ConsoleLogger(logging.NewNoOpLogger()))
	}

	mgr, err := plugins.New([]byte(config), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	disco, err := discovery.New(mgr,
		discovery.Factories(map[string]plugins.Factory{data.Name: data.Factory()}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}

func waitForStorePath(ctx context.Context, t *testing.T, store storage.Store, path string) {
	t.Helper()
	if err := util.WaitFunc(func() bool {
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath(path))
		if err != nil {
			if storage.IsNotFound(err) {
				return false
			}
			t.Fatalf("read back data: %v", err)
		}
		if cmp.Diff(map[string]any{}, act) == "" { // empty obj
			return false
		}
		return true
	}, 200*time.Millisecond, 10*time.Second); err != nil {
		t.Fatalf("wait for store path %v: %v", path, err)
	}
}

func storeWithPolicy(ctx context.Context, t *testing.T, transform string) storage.Store {
	t.Helper()
	store := inmem.New()
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "e2e.rego", []byte(transform))
	}); err != nil {
		t.Fatalf("store transform policy: %v", err)
	}
	return store
}
