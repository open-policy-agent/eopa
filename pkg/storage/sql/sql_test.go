package sql

import (
	"context"
	"testing"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"
	"github.com/open-policy-agent/opa/util/test"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

var options = Options{Driver: "sqlite", DataSourceName: "file::memory:?cache=shared"}

type testRead struct {
	path string
	exp  string
}

type testWrite struct {
	op    storage.PatchOp
	path  string
	value string
}

// testCount lets you assert the number of keys under a prefix.
// Note that we don't do exact matches, so the assertions should be
// as exact as possible:
//
//	testCount{"/foo", 1}
//	testCount{"/foo/bar", 1}
//
// both of these would be true for one element under key `/foo/bar`.
type testCount struct {
	key   string
	count int
}

func (tc *testCount) assert(t *testing.T, s *Store) {
	t.Helper()

	path := storage.MustParsePath(tc.key)
	table := s.tables.Find(path)[0]
	rpath := table.RelativePath(path)

	rows, err := s.db.Query(table.SelectQuery(rpath), table.SelectArgs(rpath)...)
	if err != nil {
		panic(err)
	}

	var count int
	for rows.Next() {
		count++
	}

	if tc.count != count {
		t.Errorf("key %v: expected %d keys, found %d", tc.key, tc.count, count)
	}
}

func TestDataTableValidation(t *testing.T) {
	closeFn := func(ctx context.Context, s *Store) {
		t.Helper()
		if s == nil {
			return
		}
		if err := s.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}

	test.WithTempFS(map[string]string{}, func(dir string) {
		ctx := context.Background()

		if _, err := New(ctx, logging.NewNoOpLogger(), nil,
			options.WithTables([]TableOpt{
				{Table: "foo_bar", Path: storage.MustParsePath("/foo/bar")},
				{Table: "foo_bar_baz", Path: storage.MustParsePath("/foo/bar/baz")},
			})); err == nil {
			t.Fatal("expected error")
		} else if sErr, ok := err.(*storage.Error); !ok {
			t.Fatal("expected storage error but got:", err)
		} else if sErr.Code != storage.InternalErr || sErr.Message != "tables overlap: [/foo/bar /foo/bar/baz]" {
			t.Fatal("unexpected code or message, got:", err)
		}

		if _, err := New(ctx, logging.NewNoOpLogger(), nil,
			options.WithTables([]TableOpt{
				{Table: "foo_bar", Path: storage.MustParsePath("/foo/bar"), KeyColumns: []string{}},
			})); err == nil {
			t.Fatal("expected error")
		} else if sErr, ok := err.(*storage.Error); !ok {
			t.Fatal("expected storage error but got:", err)
		} else if sErr.Code != storage.InternalErr || sErr.Message != "table has invalid column(s): /foo/bar" {
			t.Fatal("unexpected code or message, got:", err)
		}

		if _, err := New(ctx, logging.NewNoOpLogger(), nil,
			options.WithTables([]TableOpt{
				{Table: "foo_bar", Path: storage.MustParsePath("/foo/bar"), KeyColumns: []string{"k"}},
			})); err == nil {
			t.Fatal("expected error")
		} else if sErr, ok := err.(*storage.Error); !ok {
			t.Fatal("expected storage error but got:", err)
		} else if sErr.Code != storage.InternalErr || sErr.Message != "table has invalid column(s): /foo/bar" {
			t.Fatal("unexpected code or message, got:", err)
		}

		// set up two tables
		s, err := New(ctx, logging.NewNoOpLogger(), nil,
			options.WithTables([]TableOpt{
				{Table: "foo_bar", Path: storage.MustParsePath("/foo/bar"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}},
				{Table: "foo_baz", Path: storage.MustParsePath("/foo/baz"), KeyColumns: []string{"k1", "k2"}, ValueColumns: []ValueColumnOpt{{Column: "vv", Type: ColumnTypeJSON}}},
			}))
		if err != nil {
			t.Fatal(err)
		}

		closeFn(ctx, s.(*Store))

		// init with same settings: nothing wrong
		s, err = New(ctx, logging.NewNoOpLogger(), nil,
			options.WithTables([]TableOpt{
				{Table: "foo_bar", Path: storage.MustParsePath("/foo/bar"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}},
				{Table: "foo_baz", Path: storage.MustParsePath("/foo/baz"), KeyColumns: []string{"k1", "k2"}, ValueColumns: []ValueColumnOpt{{Column: "vv", Type: ColumnTypeJSON}}},
			}))
		if err != nil {
			t.Fatal(err)
		}

		closeFn(ctx, s.(*Store))

		// adding another table
		s, err = New(ctx, logging.NewNoOpLogger(), nil,
			options.WithTables([]TableOpt{
				{Table: "foo_bar", Path: storage.MustParsePath("/foo/bar"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}},
				{Table: "foo_baz", Path: storage.MustParsePath("/foo/baz"), KeyColumns: []string{"k1", "k2"}, ValueColumns: []ValueColumnOpt{{Column: "vv", Type: ColumnTypeJSON}}},
				{Table: "foo_qux", Path: storage.MustParsePath("/foo/qux"), KeyColumns: []string{"k3", "k4"}, ValueColumns: []ValueColumnOpt{{Column: "vvv", Type: ColumnTypeJSON}}},
			}))
		if err != nil {
			t.Fatal(err)
		}

		closeFn(ctx, s.(*Store))

		// TODO: Currently a schema change is not detected.
	})
}

func TestDataTableReadsAndWrites(t *testing.T) {
	tests := []struct {
		note     string
		tables   []TableOpt
		sequence []interface{}
	}{
		{
			note:   "exact-match: add",
			tables: []TableOpt{{Table: "foo_k1", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `"x"`,
				},
				testRead{
					path: "/foo/bar",
					exp:  `"x"`,
				},
				testCount{"/foo/bar", 1},
			},
		},
		{
			note:   "exact-match: xadd: multi-key",
			tables: []TableOpt{{Table: "foo_k1_k2", Path: storage.MustParsePath("/foo/*/*"), KeyColumns: []string{"k1", "k2"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar/baz",
					value: `"x"`,
				},
				testRead{
					path: "/foo/bar/baz",
					exp:  `"x"`,
				},
				testCount{"/foo/bar/baz", 1},
			},
		},
		{
			note:   "exact-match: remove",
			tables: []TableOpt{{Table: "foo_k3", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `7`,
				},
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/baz",
					value: `8`,
				},
				testWrite{
					op:   storage.RemoveOp,
					path: "/foo/bar",
				},
				testRead{ // prefix read
					path: "/foo",
					exp:  `{"baz": 8}`,
				},
			},
		},
		{
			note:   "read: sub-field",
			tables: []TableOpt{{Table: "foo_k4", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `{"baz": 8}`,
				},
				testRead{
					path: "/foo/bar/baz",
					exp:  `8`,
				},
				testCount{"/foo/bar", 1},
				// testCount{"/foo/bar/baz", 0},
			},
		},
		{
			note:   "prefix",
			tables: []TableOpt{{Table: "foo_k5", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo",
					value: `{"bar": 7, "baz": 8}`,
				},

				testCount{"/foo", 2},
				testRead{
					path: "/foo/bar",
					exp:  `7`,
				},
				testRead{
					path: "/foo/baz",
					exp:  `8`,
				},
				testRead{
					path: "/foo",
					exp:  `{"bar": 7, "baz": 8}`,
				},
			},
		},
		{
			note:   "prefix: overwrite",
			tables: []TableOpt{{Table: "foo_k6", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/",
					value: `{"foo": {"bar": 7, "baz": 8}}`,
				},
				testWrite{
					op:    storage.AddOp,
					path:  "/foo",
					value: `{"qux": 10, "baz": 8}`,
				},
				testRead{
					path: "/",
					exp:  `{"foo": {"qux": 10, "baz": 8}}`,
				},
			},
		},
		{
			note:   "prefix: remove",
			tables: []TableOpt{{Table: "foo_k7", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/",
					value: `{"foo": {"bar": 7, "baz": 8}}`,
				},
				testWrite{
					op:   storage.RemoveOp,
					path: "/foo",
				},
				testRead{
					path: "/",
					exp:  `{}`,
				},
			},
		},
		{
			note: "prefix: multiple tables",
			tables: []TableOpt{
				{Table: "foo_bar_k1", Path: storage.MustParsePath("/foo/bar/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}},
				{Table: "foo_baz_k1", Path: storage.MustParsePath("/foo/baz/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}},
			},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo",
					value: `{"bar": {"a": 7}, "baz": {"b": 8}}`,
				},
				testRead{
					path: "/foo/bar",
					exp:  `{"a": 7}`,
				},
				testRead{
					path: "/foo/baz",
					exp:  `{"b": 8}`,
				},
				testRead{
					path: "/foo",
					exp:  `{"bar": {"a": 7}, "baz": {"b": 8}}`,
				},
			},
		},
		// per row modifications
		{
			note:   "read-modify-write: add",
			tables: []TableOpt{{Table: "foo_k8", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `{"baz": 7}`,
				},
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar/baz",
					value: `8`,
				},
				testRead{
					path: "/foo/bar",
					exp:  `{"baz": 8}`,
				},
			},
		},
		{
			note:   "read-modify-write: add: array append",
			tables: []TableOpt{{Table: "foo_k9", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `[]`,
				},
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar/-",
					value: `8`,
				},
				testRead{
					path: "/foo/bar",
					exp:  `[8]`,
				},
			},
		},
		{
			note:   "read-modify-write: add: array append (via last index)",
			tables: []TableOpt{{Table: "foo_k10", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `[1]`,
				},
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar/1",
					value: `8`,
				},
				testRead{
					path: "/foo/bar",
					exp:  `[1, 8]`,
				},
			},
		},
		{
			note:   "read-modify-write: add: array insert",
			tables: []TableOpt{{Table: "foo_k11", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `[7]`,
				},
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar/0",
					value: `8`,
				},
				testRead{
					path: "/foo/bar",
					exp:  `[8, 7]`,
				},
			},
		},
		{
			note:   "read-modify-write: replace",
			tables: []TableOpt{{Table: "foo_k12", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `{"baz": 7}`,
				},
				testWrite{
					op:    storage.ReplaceOp,
					path:  "/foo/bar/baz",
					value: `8`,
				},
				testRead{
					path: "/foo/bar",
					exp:  `{"baz": 8}`,
				},
			},
		},
		{
			note:   "read-modify-write: replace: array",
			tables: []TableOpt{{Table: "foo_k13", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `[7]`,
				},
				testWrite{
					op:    storage.ReplaceOp,
					path:  "/foo/bar/0",
					value: `8`,
				},
				testRead{
					path: "/foo/bar",
					exp:  `[8]`,
				},
			},
		},
		{
			note:   "read-modify-write: remove",
			tables: []TableOpt{{Table: "foo_k14", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `{"baz": 7}`,
				},
				testWrite{
					op:   storage.RemoveOp,
					path: "/foo/bar/baz",
				},
				testRead{
					path: "/foo/bar",
					exp:  `{}`,
				},
			},
		},
		{
			note:   "read-modify-write: remove: array",
			tables: []TableOpt{{Table: "foo_k15", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `[7, 8]`,
				},
				testWrite{
					op:   storage.RemoveOp,
					path: "/foo/bar/0",
				},
				testRead{
					path: "/foo/bar",
					exp:  `[8]`,
				},
			},
		},
		{
			note:   "read-modify-write: multi-level: map",
			tables: []TableOpt{{Table: "foo_k16", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `{"baz": {"qux": {"corge": 7}}}`,
				},
				testWrite{
					op:    storage.ReplaceOp,
					path:  "/foo/bar/baz/qux/corge",
					value: "8",
				},
				testRead{
					path: "/foo/bar",
					exp:  `{"baz": {"qux": {"corge": 8}}}`,
				},
			},
		},
		{
			note:   "read-modify-write: multi-level: array",
			tables: []TableOpt{{Table: "foo_k17", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo/bar",
					value: `{"baz": [{"qux": {"corge": 7}}]}`,
				},
				testWrite{
					op:    storage.ReplaceOp,
					path:  "/foo/bar/baz/0/qux/corge",
					value: "8",
				},
				testRead{
					path: "/foo/bar",
					exp:  `{"baz": [{"qux": {"corge": 8}}]}`,
				},
			},
		},
		{
			note:   "issue-3711: string-to-number conversion",
			tables: []TableOpt{{Table: "foo_k18", Path: storage.MustParsePath("/foo/*"), KeyColumns: []string{"k"}, ValueColumns: []ValueColumnOpt{{Column: "v", Type: ColumnTypeJSON}}}},
			sequence: []interface{}{
				testWrite{
					op:    storage.AddOp,
					path:  "/foo",
					value: `{"2": 7}`,
				},
				testRead{
					path: "/foo/2",
					exp:  `7`,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			test.WithTempFS(map[string]string{}, func(dir string) {
				ctx := context.Background()
				s, err := New(ctx, logging.NewNoOpLogger(), nil, options.WithTables(tc.tables))
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					s.(*Store).Close(ctx)
				}()

				for _, x := range tc.sequence {
					switch x := x.(type) {
					case testCount:
						x.assert(t, s.(*Store))
					case testWrite:
						executeTestWrite(ctx, t, s, x)
					case testRead:
						result, err := storage.ReadOne(ctx, s, storage.MustParsePath(x.path))
						if err != nil {
							t.Fatal(err)
						}
						var exp fjson.Json
						if x.exp != "" {
							exp, _ = fjson.New(util.MustUnmarshalJSON([]byte(x.exp)))
						}

						if exp.Compare(fjson.MustNew(result)) != 0 {
							t.Fatalf("expected %v but got %v", x.exp, result)
						}
					default:
						panic("unexpected type")
					}
				}
			})
		})
	}
}

func executeTestWrite(ctx context.Context, t *testing.T, s storage.Store, x testWrite) {
	t.Helper()
	var val interface{}
	if x.value != "" {
		val = util.MustUnmarshalJSON([]byte(x.value))
	}
	err := storage.WriteOne(ctx, s, x.op, storage.MustParsePath(x.path), val)
	if err != nil {
		t.Fatal(err)
	}
}
