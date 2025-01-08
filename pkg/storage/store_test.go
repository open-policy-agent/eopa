//go:build use_opa_fork

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/open-policy-agent/opa/v1/config"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/util"
	"github.com/prometheus/client_golang/prometheus"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	"github.com/styrainc/enterprise-opa-private/pkg/storage/inmem"
)

var (
	_ BJSONReader     = (*store)(nil)
	_ WriterUnchecked = (*store)(nil)
	_ DataPlugins     = (*store)(nil)
)

func TestStoreRead(t *testing.T) {
	tests := []testCase{
		{
			note:     "default-only, empty storage",
			storages: []testStorage{},
			ops: []op{
				readOp{
					path: "/",
					exp:  "{}",
				},
			},
		},
		{
			note: "default-only storage",
			storages: []testStorage{
				{
					paths:   [][2]string{},
					content: `{"foo": "bar"}`,
				},
			},
			ops: []op{
				readOp{
					path: "/",
					exp:  `{"foo": "bar"}`,
				},
				readOp{
					path: "/foo",
					exp:  `"bar"`,
				},
				txnOp{
					getOp{
						key: "foo",
						exp: `"bar"`,
					},
				},
			},
		},
		{
			note: "multiple storages",
			storages: []testStorage{
				{
					paths:   [][2]string{{"/foo", "/foo/*"}},
					content: `{"foo": {"bar": "doc1", "baz": "doc2"}}`,
				},
				{
					paths:   [][2]string{{"/bar", "/bar/*"}},
					content: `{"bar": {"foo": "doc2"}}`,
				},
			},
			ops: []op{
				readOp{
					path: "/foo",
					exp:  `{"bar": "doc1", "baz": "doc2"}`,
				},
				readOp{
					path: "/bar/foo",
					exp:  `"doc2"`,
				},
				txnOp{
					getOp{
						key: "foo",
						next: getOp{
							key: "bar",
							exp: `"doc1"`,
						},
					},
				},
				txnOp{
					getOp{
						key: "bar",
						next: getOp{
							key: "foo",
							exp: `"doc2"`,
						},
					},
				},
				txnOp{
					getOp{
						key: "foo",
						next: iterOp{
							exp: `{"bar": "doc1", "baz": "doc2"}`,
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			ctx := context.Background()
			var opts Options

			root := inmem.New()

			for _, s := range tc.storages {
				if len(s.paths) == 0 {
					content := util.MustUnmarshalJSON([]byte(s.content))
					root = inmem.NewFromObject(content.(map[string]interface{}))
				} else {
					var paths [][2]storage.Path
					for _, p := range s.paths {
						paths = append(paths, [2]storage.Path{storage.MustParsePath(p[0]), storage.MustParsePath(p[1])})
					}

					content := util.MustUnmarshalJSON([]byte(s.content))
					store := inmem.NewFromObject(content.(map[string]interface{}))

					opts.Stores = append(opts.Stores, StoreOptions{
						Paths: paths,
						New: func(context.Context, logging.Logger, prometheus.Registerer, interface{}) (storage.Store, error) {
							return store, nil
						},
					})
				}
			}

			s, err := newInternal(ctx, nil, nil, root, opts)
			if err != nil {
				panic(err)
			}

			for _, op := range tc.ops {
				op.Execute(ctx, t, s.(*store))
			}
		})
	}
}

func TestStoreWrite(t *testing.T) {
	tests := []testCase{
		{
			note: "default-only storage",
			ops: []op{
				writeOp{
					path:    "/foo",
					content: `"doc"`,
				},
				readOp{
					path: "/foo",
					exp:  `"doc"`,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			ctx := context.Background()
			var opts Options

			root := inmem.New()

			for _, s := range tc.storages {
				if len(s.paths) == 0 {
					root = inmem.NewFromObject(make(map[string]interface{}))
				} else {
					var paths [][2]storage.Path
					for _, p := range s.paths {
						paths = append(paths, [2]storage.Path{storage.MustParsePath(p[0]), storage.MustParsePath(p[1])})
					}

					store := inmem.NewFromObject(make(map[string]interface{}))

					opts.Stores = append(opts.Stores, StoreOptions{
						Paths: paths,
						New: func(context.Context, logging.Logger, prometheus.Registerer, interface{}) (storage.Store, error) {
							return store, nil
						},
					})
				}
			}

			s, err := newInternal(ctx, nil, nil, root, opts)
			if err != nil {
				panic(err)
			}

			for _, op := range tc.ops {
				op.Execute(ctx, t, s.(*store))
			}
		})
	}
}

func TestStorePolicies(t *testing.T) {
	ctx := context.Background()
	s := New()
	txn, _ := s.NewTransaction(ctx, storage.WriteParams)
	if err := s.UpsertPolicy(ctx, txn, "policy", []byte("source")); err != nil {
		t.Fatal(err)
	}

	if source, err := s.GetPolicy(ctx, txn, "policy"); err != nil {
		t.Fatal(err)
	} else if string(source) != "source" {
		t.Error("incorrect get")
	}

	if policies, err := s.ListPolicies(ctx, txn); err != nil {
		t.Fatal(err)
	} else if len(policies) != 1 || policies[0] != "policy" {
		t.Error("incorrect list")
	}

	if err := s.DeletePolicy(ctx, txn, "policy"); err != nil {
		t.Fatal(err)
	}

	if policies, err := s.ListPolicies(ctx, txn); err != nil {
		t.Fatal(err)
	} else if len(policies) != 0 {
		t.Error("incorrect list")
	}
}

func TestStoreDisk(t *testing.T) {
	ctx := context.Background()
	config := config.Config{
		Storage: &struct {
			Disk json.RawMessage `json:"disk,omitempty"`
			SQL  json.RawMessage `json:"sql,omitempty"`
		}{
			Disk: json.RawMessage(fmt.Sprintf(`{"directory": "%s", "partitions": ["/a/*", "/b/c/*/*"]}`, t.TempDir())),
		},
	}

	if _, err := New2(ctx, logging.NewNoOpLogger(), nil, util.MustMarshalJSON(config), "id"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreSQL(t *testing.T) {
	ctx := context.Background()
	config := config.Config{
		Storage: &struct {
			Disk json.RawMessage `json:"disk,omitempty"`
			SQL  json.RawMessage `json:"sql,omitempty"`
		}{
			SQL: json.RawMessage(`{
"driver": "sqlite",
"data_source_name": "file::memory:?cache=shared",
"tables": [
    {"path": "/a/b", "table": "t1", "primary_key": ["c1"], "values": [{"column": "v1", "path": "/c2", "type": "string"}]},
    {"path": "/a/c", "table": "t2", "primary_key": ["c1", "c2"], "values": [{"column": "v1", "path": "/c3", "type": "string"}]}
]
}
`),
		},
	}

	_, err := New2(ctx, logging.NewNoOpLogger(), nil, util.MustMarshalJSON(config), "id")
	if err != nil {
		t.Fatal(err)
	}
}

type testCase struct {
	note     string
	storages []testStorage
	ops      []op
}

type testStorage struct {
	paths   [][2]string // if no paths given, default in-memory store used
	content string
}

type op interface {
	Execute(ctx context.Context, t *testing.T, v interface{})
}

type txnOp struct {
	next op
}

func (o txnOp) Execute(ctx context.Context, t *testing.T, v interface{}) {
	s := v.(*store)
	txn, err := s.NewTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}

	o.next.Execute(ctx, t, txn)
}

type getOp struct {
	key  string
	miss bool
	next op
	exp  string
}

type gettable interface {
	Get(ctx context.Context, key interface{}) (interface{}, bool, error)
}

func (o getOp) Execute(ctx context.Context, t *testing.T, v interface{}) {
	g := v.(gettable)
	result, hit, err := g.Get(ctx, o.key)
	if err != nil {
		t.Fatal(err)
	}

	switch {
	case !hit && o.miss:
		// Nothing found, as expected.
		return
	case !hit && !o.miss:
		t.Errorf("%v not found", o.key)
		return
	case hit && o.miss:
		t.Errorf("%v found", o.key)
		return
	case hit && !o.miss:
		if o.next != nil {
			o.next.Execute(ctx, t, result)
		} else {
			var exp fjson.Json
			exp, _ = fjson.New(util.MustUnmarshalJSON([]byte(o.exp)))
			if exp.Compare(result.(fjson.Json)) != 0 {
				t.Errorf("%v get unexpected: %v", o.key, result)
			}
		}
	}
}

type iterOp struct {
	exp string
}

type iterable interface {
	Iter(ctx context.Context, f func(key, value interface{}) (bool, error)) error
}

func (o iterOp) Execute(ctx context.Context, t *testing.T, v interface{}) {
	i := v.(iterable)
	result := fjson.NewObject(nil)

	if err := i.Iter(ctx, func(key, value interface{}) (bool, error) {
		result.Set(key.(*fjson.String).Value(), value.(fjson.Json))
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}

	exp, _ := fjson.New(util.MustUnmarshalJSON([]byte(o.exp)))
	if cmp := exp.Compare(result); cmp != 0 {
		t.Errorf("iter unexpected: %v vs %v", result, exp)
	}
}

type readOp struct {
	path string
	exp  string
}

func (o readOp) Execute(ctx context.Context, t *testing.T, v interface{}) {
	s := v.(*store)
	result, err := storage.ReadOne(ctx, s, storage.MustParsePath(o.path))

	switch {
	case storage.IsNotFound(err) && o.exp == "":
		// Nothing found as expected

	case storage.IsNotFound(err) && o.exp != "":
		t.Errorf("%v not found", o.path)

	case err != nil:
		t.Fatal(err)

	default:
		exp := util.MustUnmarshalJSON([]byte(o.exp))
		if cmp := util.Compare(result, exp); cmp != 0 {
			t.Errorf("%v read unexpected: %v", o.path, result)
		}
	}
}

type writeOp struct {
	path    string
	content string
}

func (o writeOp) Execute(ctx context.Context, t *testing.T, v interface{}) {
	s := v.(*store)
	content := util.MustUnmarshalJSON([]byte(o.content))
	err := storage.WriteOne(ctx, s, storage.AddOp, storage.MustParsePath(o.path), content)
	if err != nil {
		t.Fatal(err)
	}
}
