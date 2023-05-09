package builtins

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"

	"github.com/styrainc/load-private/pkg/vm"
)

func TestSQLSend(t *testing.T) {
	file := t.TempDir() + "/sqlite3.db"
	populate(t, file)

	tests := []struct {
		note   string
		source string
		result string
		error  string
	}{
		{
			"missing parameter(s)",
			`p = resp { sql.send({}, resp)}`,
			"",
			`eval_type_error: sql.send: operand 1 missing required request parameters(s): {"data_source_name", "driver", "query"}`},
		{
			"a single row query",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM T1"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["A", "B"]]}}}}`,
			"",
		},
		{
			"a multi-row query",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT * FROM T2"}, resp)}`, file),
			`{{"result": {"p": {"rows": [["A1", "B1"], ["A2", "B2"]]}}}}`,
			"",
		},
		{
			"query with args",
			fmt.Sprintf(`p = resp { sql.send({"driver": "sqlite", "data_source_name": "%s", "query": "SELECT VALUE FROM T1 WHERE KEY = $1", "args": ["A"]}, resp)}`, file),
			`{{"result": {"p": {"rows": [["B"]]}}}}`,
			"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			execute(t, "package t\n"+tc.source, "t", tc.result, tc.error)
		})
	}
}

func execute(tb testing.TB, module string, query string, expectedResult string, expectedError string) {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(module),
				Parsed: ast.MustParseModule(module),
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(query)
	if err := compiler.Build(context.Background()); err != nil {
		tb.Fatal(err)
	}

	var policy ir.Policy
	if err := json.Unmarshal(compiler.Bundle().PlanModules[0].Raw, &policy); err != nil {
		tb.Fatal(err)
	}

	executable, err := vm.NewCompiler().WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}

	_, ctx := vm.WithStatistics(context.Background())
	v, err := vm.NewVM().WithExecutable(executable).Eval(ctx, query, vm.EvalOpts{StrictBuiltinErrors: true})
	if expectedError != "" {
		if expectedError != err.Error() {
			tb.Fatalf("unexpected error: %v", err)
		}

		return
	}
	if err != nil {
		tb.Fatal(err)
	}

	if t := ast.MustParseTerm(expectedResult); v.Compare(t.Value) != 0 {
		tb.Fatalf("got %v wanted %v\n", v, expectedResult)
	}
}

func populate(tb testing.TB, file string) {
	db, err := sql.Open("sqlite", file)
	if err != nil {
		tb.Fatal(err)
	}
	defer db.Close()

	sql := `
        CREATE TABLE T1 (KEY TEXT, VALUE TEXT);
        CREATE TABLE T2 (KEY TEXT, VALUE TEXT);

        INSERT INTO T1(KEY, VALUE) VALUES("A", "B");
        INSERT INTO T2(KEY, VALUE) VALUES("A1", "B1");
        INSERT INTO T2(KEY, VALUE) VALUES("A2", "B2");
	`
	if _, err = db.Exec(sql); err != nil {
		tb.Fatal(err)
	}
}
