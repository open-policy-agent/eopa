package localfile_test

import (
	"context"
	gojson "encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/open-policy-agent/opa/v1/util/test"

	common "github.com/styrainc/enterprise-opa-private/pkg/internal/goleak"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, common.Defaults...)
}

func TestLocalFileData(t *testing.T) {
	t.Parallel()

	const transform = `package e2e
	transform.users[id] := d if {
		entry := input.incoming
		id := entry.id
		d := entry.userId
	}
	`

	testcases := []struct {
		name               string
		filename           string
		initialFileContent string
		updatedContents    []string
		config             string
		expectedData       []map[string]any
	}{
		{
			name: "simple",
			config: `
plugins:
  data:
    localfile.placeholder:
      type: localfile
      file_path: %[1]s/simple.json
`,
			expectedData: []map[string]any{
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
				},
			},
			filename: "simple.json",
			initialFileContent: `
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
		},
		{
			name: "transform",
			config: `
plugins:
  data:
    localfile.placeholder:
      type: localfile
      file_path: %[1]s/transform_test.json
      rego_transform: data.e2e.transform
`,
			expectedData: []map[string]any{
				{
					"users": map[string]any{
						"id01": "admin",
					},
				},
			},
			filename: "transform_test.json",
			initialFileContent: `
{
  "userId": "admin",
  "id": "id01",
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
		},
		{
			name: "two polls - json",
			config: `
plugins:
  data:
    localfile.placeholder:
      type: localfile
      file_path: %[1]s/two_polls.json
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
			filename: "two_polls.json",
			initialFileContent: `
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit"
}`,
			updatedContents: []string{`
{
  "userId": 1,
  "id": 1,
  "title": "quia et suscipit suscipit recusandae consequuntur expedita et cum reprehenderit molestiae ut ut quas totam nostrum rerum est autem sunt rem eveniet architecto"
}`},
		},
		{
			name: "two polls - yaml",
			config: `
plugins:
  data:
    localfile.placeholder:
      type: localfile
      file_path: %[1]s/two_polls.yaml
      polling_interval: 1s
`,
			expectedData: []map[string]any{
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
				},
				{
					"userId": gojson.Number("2"),
					"id":     gojson.Number("2"),
					"title":  "quia et suscipit suscipit recusandae consequuntur expedita et cum reprehenderit molestiae ut ut quas totam nostrum rerum est autem sunt rem eveniet architecto",
				},
			},
			filename: "two_polls.yaml",
			initialFileContent: `
---
userId: 1
id: 1
title: "sunt aut facere repellat provident occaecati excepturi optio reprehenderit"
`,
			updatedContents: []string{`
---
userId: 2
id: 2
title: "quia et suscipit suscipit recusandae consequuntur expedita et cum reprehenderit molestiae ut ut quas totam nostrum rerum est autem sunt rem eveniet architecto"
`},
		},
		{
			name: "simple yaml, explicit filetype with wrong file extension",
			config: `
plugins:
  data:
    localfile.placeholder:
      type: localfile
      file_path: %[1]s/simple-yaml.json
      file_type: yaml
`,
			expectedData: []map[string]any{
				{
					"userId": gojson.Number("1"),
					"id":     gojson.Number("1"),
					"title":  "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
				},
			},
			filename: "simple-yaml.json",
			initialFileContent: `
---
userId: 1
id: 1
title: "sunt aut facere repellat provident occaecati excepturi optio reprehenderit"
`,
		},
	}

	// Create initial files map for the temp file system to use.
	fileContents := make(map[string]string, len(testcases))
	for _, tc := range testcases {
		fileContents[tc.filename] = tc.initialFileContent
	}

	test.WithTempFS(fileContents, func(rootPath string) {
		for _, tc := range testcases {
			t.Run(tc.name, func(t *testing.T) {
				ctx := context.Background()
				config := fmt.Sprintf(tc.config, rootPath)

				store := storeWithPolicy(ctx, t, transform)
				mgr := pluginMgr(t, store, config)

				if err := mgr.Start(ctx); err != nil {
					t.Fatal(err)
				}
				defer mgr.Stop(ctx)

				waitForStorePath(ctx, t, store, "/localfile/placeholder")

				if len(tc.updatedContents) >= len(tc.expectedData) {
					panic(fmt.Sprintf("test case %s has more updates (%d) than expected results (%d)", tc.name, len(tc.updatedContents), len(tc.expectedData)))
				}

				for i, exp := range tc.expectedData {
					act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/localfile/placeholder"))
					if err != nil {
						t.Fatalf("read back data: %v", err)
					}
					if diff := cmp.Diff(exp, act); diff != "" {
						t.Errorf("data value mismatch for #%d, diff:\n%s", i, diff)
					}
					// Write an update to the file, if one is available.
					if len(tc.updatedContents) > 0 && i < len(tc.updatedContents) {
						writeFile(filepath.Join(rootPath, tc.filename), tc.updatedContents[i])
					}
					// Wait until next interval, except when this is the last expected item
					if i != len(tc.expectedData)-1 {
						time.Sleep(1 * time.Second)
					}
				}
			})
		}
	})
}

func TestLocalFileOwned(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"owned.json": `
{
  "userId": 1,
  "id": 1,
  "title": "sunt aut facere repellat provident occaecati excepturi optio reprehenderit",
}`,
	}

	config := `
plugins:
  data:
    localfile.placeholder:
      type: localfile
      file_path: %[1]s/owned.json
`

	test.WithTempFS(files, func(rootPath string) {
		ctx := context.Background()
		cfg := fmt.Sprintf(config, rootPath)

		store := inmem.New()
		mgr := pluginMgr(t, store, cfg)

		if err := mgr.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer mgr.Stop(ctx)

		// test owned path
		err := storage.WriteOne(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/localfile/placeholder"), map[string]any{"foo": "bar"})
		if err == nil || err.Error() != `path "/localfile/placeholder" is owned by plugin "localfile"` {
			t.Errorf("owned check failed, got %v", err)
		}
	})
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

func writeFile(path, contents string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	if _, err := f.WriteString(contents); err != nil {
		return err
	}

	return nil
}
