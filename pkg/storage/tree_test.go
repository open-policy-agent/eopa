package storage

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/load-private/pkg/json"
	inmem "github.com/styrainc/load-private/pkg/store"
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
		results []string // expected iteration keys
		len     string
		n       int
		find    string
		store   string // reference to the store to return
	}

	type test struct {
		inserts    []insert
		operations []operation
	}

	tests := []test{
		{
			// root only, default storage
			[]insert{
				{"/", 0, `{"a": {"b": "c"}}`},
			},
			[]operation{
				{get: "/a/b", result: `"c"`},
				{iter: "/", results: []string{"a"}},
				{iter: "/a", results: []string{"b"}},
				{len: "/", n: 1},
				{len: "/a", n: 1},
				{find: "/", store: ""},
				{find: "/a", store: ""},
			},
		},
		{
			// two additional storages attached to /a/b and /a/c
			[]insert{
				{"/a/b", 3, `{"a": {"b": {"c": "d"}}}`},
				{"/a/c", 4, `{"a": {"c": {"d": {"e": "f"}}}}`},
				{"/", 0, `{"b": {"c": "d"}}`},
			},
			[]operation{
				{get: "/a/b/c", result: `"d"`},
				{get: "/a/c/d/e", result: `"f"`},
				{iter: "/", results: []string{"a", "b"}},
				{iter: "/a", results: []string{"b", "c"}},
				{iter: "/a/b", results: []string{"c"}},
				{iter: "/a/c", results: []string{"d"}},
				{iter: "/a/c/d", results: []string{"e"}},
				{len: "/", n: 2},
				{len: "/a", n: 2},
				{len: "/a/b", n: 1},
				{len: "/a/c", n: 1},
				{len: "/a/c/d", n: 1},
				{find: "/", store: ""},
				{find: "/a", store: "a"},
				{find: "/a/b", store: "/a/b"},
				{find: "/a/b/c", store: "/a/b"},
				{find: "/a/c", store: "/a/c"},
				{find: "/a/c/d", store: "/a/c"},
			},
		},
	}

	for _, test := range tests {
		ctx := context.Background()
		tree := newTree(nil)
		storages := make(map[string]storage.Store)
		root := inmem.New()

		for _, insert := range test.inserts {
			path := storage.MustParsePath(insert.Path)
			store := inmem.NewFromReader(bytes.NewBufferString(insert.Doc))
			if len(path) == 0 {
				root = store
				continue
			}

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

				var keys []string
				var err error

				switch i := result.(type) {
				case node:
					err = i.Iter(ctx, func(key, value interface{}) bool {
						keys = append(keys, key.(*json.String).Value())
						return false
					})
				case json.Object:
					keys = i.Names()
				}

				if err != nil {
					t.Fatal(err)
				} else if !reflect.DeepEqual(keys, op.results) {
					t.Errorf("unexpected results for %s: %v", op.iter, keys)
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

				if err != nil {
					t.Fatal(err)
				} else if n != op.n {
					t.Errorf("unexpected len for %s: %v", op.len, n)
				}

			case op.find != "":
				found := tree.Find(storage.MustParsePath(op.find))
				expected := storages[op.store]

				if found != expected {
					t.Errorf("unexpected store found for %s: %v", op.find, found)
				}
			}
		}
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
