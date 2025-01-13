package compile_test

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	go_cmp "github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/logging/test"
	"github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/test/e2e"
	"github.com/styrainc/enterprise-opa-private/pkg/compile"
)

// Error is needed here because the ast.Error type cannot be
// unmarshalled from JSON: it contains an interface value.
type Error struct {
	Code     string          `json:"code"`
	Message  string          `json:"message"`
	Location *ast.Location   `json:"location,omitempty"`
	Details  compile.Details `json:"details,omitempty"`
}

// NOTE(sr): The important thing about these tests is that we don't mock
// the partially-evaluated Rego. Instead, we store the data filter policy,
// run the PE-post-analysing handler, and have assertions on its response.
func TestPostPartialChecks(t *testing.T) {
	const defaultQuery = "data.filters.include"
	defaultInput := map[string]any{
		"a": true,
		"b": false,
	}
	defaultUnknowns := []string{"input.fruits", "input.baskets"}
	for _, tc := range []struct {
		note     string
		rego     string
		unknowns []string
		input    any
		query    string
		errors   []Error
		skip     string
	}{
		{
			note: "happy path",
			rego: `include if input.fruits.colour == "orange"`,
		},
		{
			note: "happy path, reversed",
			rego: `include if "orange" == input.fruits.colour`,
		},
		{
			note: "invalid builtin",
			rego: `include if object.get(input, ["fruits", "colour"], "grey") == "orange"`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "invalid builtin object.get",
				},
			},
		},
		{
			note: "invalid use of 'v in...'",
			rego: `include if input.fruits.colour in {"grey", "orange"}`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 32),
					Message:  "invalid use of \"... in ...\"",
				},
			},
		},
		{
			note: "invalid use of 'k, v in...'",
			rego: `include if "k", input.fruits.colour in {"k": "grey", "k2": "orange"}`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 37),
					Message:  "invalid use of \"... in ...\"",
				},
			},
		},
		{
			note: "nested comp",
			rego: `include if (input.fruits.colour == "orange")>0`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 45),
					Message:  `gt: nested call operand: equal(input.fruits.colour, "orange")`, // TODO(sr): make this a user-friendlier message
				},
			},
		},
		{
			note: "nested call, object.get",
			rego: `user := object.get(input, ["user"], "unknown")
include if user == input.fruits.user`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 9),
					Message:  `eq: nested call operand: object.get(input, ["user"], "unknown")`, // TODO(sr): make this a user-friendlier message
				},
			},
		},
		{
			note: "rhs+lhs both unknown",
			rego: `include if input.fruits.colour == input.baskets.colour`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12), // NOTE(sr): 12 is the `i` of `input.fruits.colour` -- it would be nicer if had location spans here
					Message:  "both rhs and lhs non-scalar/non-ground",
				},
			},
		},
		{
			note: "contains: rhs unknown",
			rego: `include if contains("foobar", input.fruits.colour)`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "rhs of contains must be scalar",
				},
			},
		},
		{
			note: "startswith: rhs unknown",
			rego: `include if startswith("foobar", input.fruits.colour)`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "rhs of startswith must be scalar",
				},
			},
		},
		{
			note: "endswith: rhs unknown",
			rego: `include if endswith("foobar", input.fruits.colour)`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "rhs of endswith must be scalar",
				},
			},
		},
		{
			note: "non-scalar comparison",
			rego: `include if input.fruits.colour <= {"green", "blue"}`, // nonsense, but still
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 32), // NOTE(sr): 32 is the `<=`
					Message:  "both rhs and lhs non-scalar/non-ground",
				},
			},
		},
		{
			note: "not a call/term",
			rego: `include if input.fruits.colour`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "invalid statement \"input.fruits.colour\"",
					Details:  compile.Details{Extra: "try `input.fruits.colour != false`"},
				},
			},
		},
		{
			note: "not a call/term, using with",
			rego: `include if {
	foo with input.fruits.colour as "red"
}
foo if input.fruits.colour`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 4, 2),
					Message:  "\"with\" not permitted",
				},
			},
		},
		{
			note: "not a call/every",
			rego: `include if every x in input.fruits.xs { x != "y" }`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "\"every\" not permitted",
				},
			},
		},
		{
			note: "reference other row",
			rego: `include if {
   some other in input.fruits
   input.fruits.price > other.price
}`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 5, 23),
					Message:  "both rhs and lhs non-scalar/non-ground", // NOTE(sr): at least it's caught, the message could be improved
				},
			},
		},
		{
			note: "support module: default rule",
			rego: `include if other
default other := false
other if input.fruits.price > 100
`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "use of default rule in data.filters.other",
				},
			},
		},
		{
			note: "support module: multi-value rule",
			rego: `include if mv
mv contains 1 if input.fruits.price <= 1
mv contains 2 if input.fruits.price <= 2
`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "use of multi-value rule in data.filters.mv",
				},
			},
		},
		{
			// NOTE(sr): This could lead to data policies getting accepted _now_ that at
			// some later time -- when the bug is fixed -- would no longer be valid.
			note: "support module: default function",
			rego: `include if cheap(input.fruits)
default cheap(_) := true
cheap(f) if f.price < 100
`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "use of default rule in data.filters.other",
				},
			},
			skip: `https://github.com/open-policy-agent/opa/issues/7220`,
		},
		{
			note: "ref into module: complete rule with else",
			rego: `include if other
other if input.fruits.price > 100
else := input.fruits.extra
`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "invalid data reference \"data.filters.other\"",
					Details:  compile.Details{Extra: "has rule \"data.filters.other\" an `else`?"},
				},
			},
		},
		{
			note: "ref into module: function with else",
			rego: `include if func(input.fruits)
func(f) if f.price > 100
else := true
`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "invalid data reference \"data.filters.func(input.fruits)\"",
					Details:  compile.Details{Extra: "has function \"data.filters.func(...)\" an `else`?"},
				},
			},
		},
		{
			// NOTE(sr): this seems like one of the lower-hanging fruit for translating:
			// queries: [not data.partial.__not1_0_2__]
			// support:
			//   package partial
			//   __not1_0_2__ if input.fruits.price > 100
			note: "invalid expression: complete rule with not",
			rego: `include if not other
other if input.fruits.price > 100
`,
			errors: []Error{
				{
					Code:     "pe_fragment_error",
					Location: ast.NewLocation(nil, "filters.rego", 3, 12),
					Message:  "\"not\" not permitted",
				},
			},
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			if tc.skip != "" {
				t.Skip(tc.skip)
			}
			unk := tc.unknowns
			if len(unk) == 0 {
				unk = defaultUnknowns
			}
			rego := "package filters\nimport rego.v1\n" + tc.rego
			runHandler(t,
				rego,
				cmp.Or(tc.query, defaultQuery),
				cmp.Or(tc.input, any(defaultInput)),
				unk,
				tc.errors,
			)
		})
	}
}

func runHandler(t *testing.T, rego, query string, input any, unknowns []string, errs []Error) {
	ctx := context.Background()
	payload := types.CompileRequestV1{
		Input:    &input,
		Query:    query,
		Unknowns: &unknowns,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	l := test.New()
	l.SetLevel(logging.Debug)
	chnd := compile.Handler(l)
	params := e2e.NewAPIServerTestParams()
	params.Logger = l
	trt, err := e2e.NewTestRuntime(params)
	if err != nil {
		t.Fatalf("test runtime: %v", err)
	}
	t.Cleanup(trt.Cancel)

	txn := storage.NewTransactionOrDie(ctx, trt.Runtime.Store, storage.WriteParams)
	if err := trt.Runtime.Store.UpsertPolicy(ctx, txn, "filters.rego", []byte(rego)); err != nil {
		t.Fatalf("upsert policy: %v", err)
	}
	if err := trt.Runtime.Store.Commit(ctx, txn); err != nil {
		t.Fatalf("store policy: %v", err)
	}
	chnd.SetRuntime(trt.Runtime)

	req, err := http.NewRequest("POST", "/exp/compile", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	chnd.ServeHTTP(rr, req)

	{
		exp := http.StatusOK
		if len(errs) > 0 {
			exp = http.StatusBadRequest
		}
		if act := rr.Code; exp != act {
			t.Fatalf("status code: expected %d, got %d", exp, act)
		}
	}

	{
		exp := errs
		var resp struct {
			Errors []Error `json:"errors"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		act := resp.Errors

		// NOTE(sr): If the test run gives a nil-pointer exception panic, pass this as third
		// argument to `go_cmp.Diff` -- it's probably that Location is empty for the actual
		// response.
		// --> cmpopts.IgnoreFields(Error{}, "Location") <--
		if diff := go_cmp.Diff(exp, act); diff != "" {
			t.Errorf("response unexpected (-want, +got):\n%s", diff)
		}
	}
}
