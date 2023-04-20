package storage

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/load-private/pkg/json"
	"github.com/styrainc/load-private/pkg/storage/inmem"
	"github.com/styrainc/load-private/pkg/vm"
)

func TestTree(t *testing.T) {
	type insert struct {
		Path         string
		CompletePath int
		Doc          string
	}

	type operation struct {
		get     string
		result  string
		iter    string
		results interface{} // expected iteration keys or an error
		len     string
		n       int
		find    string
		store   string // reference to the store to return
		err     error
	}

	type test struct {
		inserts    []insert
		operations []operation
	}

	tests := []test{
		{
			// Root only storage. In this setup, there are no restrictions around
			// accessing the storage as all calls will hit only a single store.
			[]insert{
				{"/", 0, `{"a": {"b": "c"}}`},
			},
			[]operation{
				// get, iter, len constitute the "namespace" interface for the VM to use in accesing the store.
				{get: "/a/b", result: `"c"`},
				{iter: "/", results: []string{"a"}},
				{iter: "/a", results: []string{"b"}},
				{len: "/", n: 1},
				{len: "/a", n: 1},
				// find backs the Read/Write operations and what these operations can access.
				{find: "/", store: "/"},  // specific enough to match to root
				{find: "/a", store: "/"}, // ditto
				{find: "/b", store: "/"}, // ditto
			},
		},
		{
			// Two storages attached to /a/b and /a/c, in addition to the root.
			// Too unspecific operations are disallowed as they can't determine the store
			// without ambiguity.
			[]insert{
				{"/a/b", 3, `{"a": {"b": {"c": "d"}}}`},
				{"/a/c", 4, `{"a": {"c": {"d": {"e": "f"}}}}`},
				{"/", 0, `{"b": {"c": "d"}}`},
			},
			[]operation{
				{get: "/a/b/c", result: `"d"`},
				{get: "/a/c/d/e", result: `"f"`},
				{iter: "/", err: &storage.Error{Code: ReadsNotSupportedErr, Message: "/"}},
				{iter: "/a", err: &storage.Error{Code: ReadsNotSupportedErr, Message: "/a"}},
				{iter: "/a/b", results: []string{"c"}},
				{iter: "/a/c", results: []string{"d"}},
				{iter: "/a/c/d", results: []string{"e"}},
				{len: "/", err: &storage.Error{Code: ReadsNotSupportedErr, Message: "/"}},
				{len: "/a", err: &storage.Error{Code: ReadsNotSupportedErr, Message: "/a"}},
				{len: "/a/b", n: 1},
				{len: "/a/c", n: 1},
				{len: "/a/c/d", n: 1},
				{find: "/", err: &storage.Error{Code: ReadsNotSupportedErr, Message: "/"}},   // not specific enough to determine the store
				{find: "/a", err: &storage.Error{Code: ReadsNotSupportedErr, Message: "/a"}}, // no store at this level
				{find: "/a/b", store: "/a/b"},
				{find: "/a/b/c", store: "/a/b"},
				{find: "/a/c", store: "/a/c"},
				{find: "/a/c/d", store: "/a/c"},
				{find: "/a/d", err: &storage.Error{Code: ReadsNotSupportedErr, Message: "/a/d"}},
				{find: "/b", store: "/"}, // specific enough to determine the store
			},
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			ctx := context.Background()
			root := inmem.New()
			tree := newTree(nil, root)
			storages := make(map[string]storage.Store)

			for _, insert := range test.inserts {
				path := storage.MustParsePath(insert.Path)
				doc, _ := json.NewDecoder(bytes.NewBufferString(insert.Doc)).Decode()

				if len(path) == 0 {
					txn, _ := root.NewTransaction(ctx, storage.WriteParams)
					root.Write(ctx, txn, storage.AddOp, storage.Path{}, doc)
					root.Commit(ctx, txn)
					storages[insert.Path] = root
					continue
				}

				store := inmem.NewFromObject(doc.(json.Object))
				lt := newLazyTree(insert.CompletePath, path, store, nil)
				if err := tree.Insert(path, lt); err != nil {
					t.Fatal(err)
				}

				storages[insert.Path] = store
			}

			store := store{root: root}
			txn := &transaction{xid: 0, store: &store, params: []storage.TransactionParams{storage.WriteParams}}
			for _, op := range test.operations {
				switch {
				case op.get != "":
					result := traverse(ctx, tree, txn, op.get)
					if expected, _ := json.NewDecoder(bytes.NewBufferString(op.result)).Decode(); expected.Compare(result.(json.Json)) != 0 {
						t.Errorf("unxpected result for %s: %v", op.get, result)
					}

				case op.iter != "":
					result := traverse(ctx, tree, txn, op.iter)

					var keys interface{}
					var err error

					switch i := result.(type) {
					case node:
						err = i.Iter(ctx, func(key, value interface{}) bool {
							if keys == nil {
								keys = make([]string, 0)
							}
							keys = append(keys.([]string), key.(*json.String).Value())
							return false
						})
					case json.Object:
						keys = i.Names()
					}

					if !reflect.DeepEqual(err, op.err) {
						t.Errorf("unexpected error for %s: %v", op.iter, err)
					}

					if !reflect.DeepEqual(keys, op.results) {
						t.Errorf("unexpected iteration results for %s: %v", op.iter, keys)
					}

					// TODO: check iteration values

				case op.len != "":
					result := traverse(ctx, tree, txn, op.len)
					var n int
					var err error

					switch i := result.(type) {
					case node:
						n, err = i.Len(ctx)
					case json.Object:
						n = i.Len()
					}

					if !reflect.DeepEqual(err, op.err) {
						t.Errorf("unexpected error for %s: %v", op.len, err)
					}

					if n != op.n {
						t.Errorf("unexpected len for %s: %v", op.len, n)
					}

				case op.find != "":
					found, err := tree.Find(storage.MustParsePath(op.find))

					var expected storage.Store
					if op.store != "" {
						expected = storages[op.store]
						if found != expected {
							t.Errorf("unexpected store found for %s: %v", op.find, found)
						}
					} else {
						if found != nil {
							t.Errorf("unexpected store found for %s: %v", op.find, found)
						}

					}

					if !reflect.DeepEqual(err, op.err) {
						t.Errorf("unexpected error for %s: %v", op.find, err)
					}
				}
			}
		})
	}
}

func traverse(ctx context.Context, tree *tree, txn *transaction, path string) interface{} {
	var result interface{} = tree.Clone(txn)

	for _, seg := range storage.MustParsePath(path) {
		var ok bool
		switch n := result.(type) {
		case vm.IterableObject:
			result, ok, _ = n.Get(ctx, seg)
		case json.Object:
			result = n.Value(seg)
			ok = result != nil
		}

		if !ok {
			panic("not found")
		}
	}

	return result
}
