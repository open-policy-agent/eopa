package git_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
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

func TestGitData(t *testing.T) {
	t.Parallel()

	const transform = `package e2e
	transform.raw := input.incoming
`

	for _, tt := range []struct {
		name     string
		config   string
		validate func(t *testing.T, store storage.Store, repo *git.Repository, dir string)
	}{
		{
			name: "one file",
			config: `
plugins:
  data:
    git.placeholder:
      type: git
      url: %s
      polling_interval: 10s
      file_path: data.json
`,
			validate: func(t *testing.T, store storage.Store, repo *git.Repository, dir string) {
				ctx := context.Background()

				w, err := repo.Worktree()
				if err != nil {
					t.Fatalf("getting worktree")
				}

				if err := os.WriteFile(path.Join(dir, "data.json"), []byte(`{"foo": "bar"}`), 0o644); err != nil {
					t.Fatalf("create data.json file: %v", err)
				}
				expected := map[string]any{
					"foo": "bar",
				}

				if _, err := w.Add("data.json"); err != nil {
					t.Fatalf("adding data.json to commit: %v", err)
				}
				if _, err := w.Commit("add data.json", &git.CommitOptions{
					Author: &object.Signature{
						Name:  "John Doe",
						Email: "john@doe.org",
						When:  time.Now(),
					},
				}); err != nil {
					t.Fatalf("creating new commit: %v", err)
				}

				waitForStorePath(ctx, t, store, "/git/placeholder")
				act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/git/placeholder"))
				if err != nil {
					t.Fatalf("read back data: %v", err)
				}
				if diff := cmp.Diff(expected, act); diff != "" {
					t.Errorf("data value mismatch, diff:\n%s", diff)
				}

				// update file

				if err := os.WriteFile(path.Join(dir, "data.json"), []byte(`{"foo": "baz"}`), 0o644); err != nil {
					t.Fatalf("create data.json file: %v", err)
				}
				expected = map[string]any{
					"foo": "baz",
				}

				if _, err := w.Add("data.json"); err != nil {
					t.Fatalf("adding data.json to commit: %v", err)
				}
				if _, err := w.Commit("add data.json", &git.CommitOptions{
					Author: &object.Signature{
						Name:  "John Doe",
						Email: "john@doe.org",
						When:  time.Now(),
					},
				}); err != nil {
					t.Fatalf("creating new commit: %v", err)
				}

				time.Sleep(12 * time.Second)
				act, err = storage.ReadOne(ctx, store, storage.MustParsePath("/git/placeholder"))
				if err != nil {
					t.Fatalf("read back data: %v", err)
				}
				if diff := cmp.Diff(expected, act); diff != "" {
					t.Errorf("data value mismatch, diff:\n%s", diff)
				}
			},
		},
		{
			name: "transform",
			config: `
plugins:
  data:
    git.placeholder:
      type: git
      url: %s
      polling_interval: 10s
      file_path: data.json
      rego_transform: data.e2e.transform
`,
			validate: func(t *testing.T, store storage.Store, repo *git.Repository, dir string) {
				ctx := context.Background()

				w, err := repo.Worktree()
				if err != nil {
					t.Fatalf("getting worktree")
				}

				if err := os.WriteFile(path.Join(dir, "data.json"), []byte(`{"foo": "bar"}`), 0o644); err != nil {
					t.Fatalf("create data.json file: %v", err)
				}
				expected := map[string]any{
					"raw": map[string]any{
						"foo": "bar",
					},
				}

				if _, err := w.Add("data.json"); err != nil {
					t.Fatalf("adding data.json to commit: %v", err)
				}
				if _, err := w.Commit("add data.json", &git.CommitOptions{
					Author: &object.Signature{
						Name:  "John Doe",
						Email: "john@doe.org",
						When:  time.Now(),
					},
				}); err != nil {
					t.Fatalf("creating new commit: %v", err)
				}

				waitForStorePath(ctx, t, store, "/git/placeholder")
				act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/git/placeholder"))
				if err != nil {
					t.Fatalf("read back data: %v", err)
				}
				if diff := cmp.Diff(expected, act); diff != "" {
					t.Errorf("data value mismatch, diff:\n%s", diff)
				}

				// update file

				if err := os.WriteFile(path.Join(dir, "data.json"), []byte(`{"foo": "baz"}`), 0o644); err != nil {
					t.Fatalf("create data.json file: %v", err)
				}
				expected = map[string]any{
					"raw": map[string]any{
						"foo": "baz",
					},
				}

				if _, err := w.Add("data.json"); err != nil {
					t.Fatalf("adding data.json to commit: %v", err)
				}
				if _, err := w.Commit("add data.json", &git.CommitOptions{
					Author: &object.Signature{
						Name:  "John Doe",
						Email: "john@doe.org",
						When:  time.Now(),
					},
				}); err != nil {
					t.Fatalf("creating new commit: %v", err)
				}

				time.Sleep(12 * time.Second)
				act, err = storage.ReadOne(ctx, store, storage.MustParsePath("/git/placeholder"))
				if err != nil {
					t.Fatalf("read back data: %v", err)
				}
				if diff := cmp.Diff(expected, act); diff != "" {
					t.Errorf("data value mismatch, diff:\n%s", diff)
				}
			},
		},
		{
			name: "json, yaml and xml files in dirs",
			config: `
plugins:
  data:
    git.placeholder:
      type: git
      url: %s
      polling_interval: 10s
`,
			validate: func(t *testing.T, store storage.Store, repo *git.Repository, dir string) {
				ctx := context.Background()

				w, err := repo.Worktree()
				if err != nil {
					t.Fatalf("getting worktree")
				}

				if err := os.Mkdir(path.Join(dir, "foo"), 0o755); err != nil {
					t.Fatalf("create foo folder: %v", err)
				}
				if err := os.WriteFile(path.Join(dir, "foo", "foo.json"), []byte(`{"foo": "json"}`), 0o644); err != nil {
					t.Fatalf("create foo/foo.json file: %v", err)
				}
				if err := os.Mkdir(path.Join(dir, "bar"), 0o755); err != nil {
					t.Fatalf("create bar folder: %v", err)
				}
				if err := os.WriteFile(path.Join(dir, "bar", "bar.yaml"), []byte(`bar: yaml`), 0o644); err != nil {
					t.Fatalf("create bar/bxxar.yaml file: %v", err)
				}
				if err := os.WriteFile(path.Join(dir, "bar", "bar.yml"), []byte(`bar: yml`), 0o644); err != nil {
					t.Fatalf("create bar/bar.yml file: %v", err)
				}
				if err := os.Mkdir(path.Join(dir, "baz"), 0o755); err != nil {
					t.Fatalf("create baz folder: %v", err)
				}
				if err := os.WriteFile(path.Join(dir, "baz", "baz.xml"), []byte(`<baz>xml</baz>`), 0o644); err != nil {
					t.Fatalf("create baz/baz.xml file: %v", err)
				}
				expected := map[string]any{
					"bar": map[string]any{
						"bar.yaml": map[string]any{
							"" +
								"bar": "yaml",
						},
						"bar.yml": map[string]any{
							"bar": "yml",
						},
					},
					"baz": map[string]any{
						"baz.xml": map[string]any{
							"baz": "xml",
						},
					},
					"foo": map[string]any{
						"foo.json": map[string]any{
							"foo": "json",
						},
					},
				}

				if err := w.AddWithOptions(&git.AddOptions{
					All:  true,
					Path: ".",
				}); err != nil {
					t.Fatalf("adding data.json to commit: %v", err)
				}
				if _, err := w.Commit("add files", &git.CommitOptions{
					Author: &object.Signature{
						Name:  "John Doe",
						Email: "john@doe.org",
						When:  time.Now(),
					},
				}); err != nil {
					t.Fatalf("creating new commit: %v", err)
				}

				waitForStorePath(ctx, t, store, "/git/placeholder")
				act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/git/placeholder"))
				if err != nil {
					t.Fatalf("read back data: %v", err)
				}
				if diff := cmp.Diff(expected, act); diff != "" {
					t.Errorf("data value mismatch, diff:\n%s", diff)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfg := fmt.Sprintf(tt.config, dir)
			repo, err := git.PlainInit(dir, false)
			if err != nil {
				t.Fatalf("creating repo failed: %+v", err)
			}

			ctx := context.Background()
			store := storeWithPolicy(ctx, t, transform)
			mgr := pluginMgr(t, store, cfg)

			if err := mgr.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer mgr.Stop(ctx)

			tt.validate(t, store, repo, dir)
		})
	}
}

func TestGitOwned(t *testing.T) {
	t.Parallel()

	config := `
plugins:
  data:
    git.placeholder:
      type: git
      url: %s
      polling_interval: 10s
      file_path: data.json
`

	dir := t.TempDir()
	cfg := fmt.Sprintf(config, dir)

	ctx := context.Background()
	store := inmem.New()
	mgr := pluginMgr(t, store, cfg)

	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	// test owned path
	err := storage.WriteOne(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/git/placeholder"), map[string]any{"foo": "bar"})
	if err == nil || err.Error() != `path "/git/placeholder" is owned by plugin "git"` {
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
	}, 200*time.Millisecond, 30*time.Second); err != nil {
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
