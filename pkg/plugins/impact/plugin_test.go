package impact_test

import (
	"context"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/loader"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"

	common "github.com/open-policy-agent/eopa/pkg/internal/goleak"
	"github.com/open-policy-agent/eopa/pkg/plugins/impact"
)

func TestStop(t *testing.T) {
	defer goleak.VerifyNone(t, common.Defaults...)
	ctx := context.Background()

	mgr := pluginMgr(t, `
plugins:
  impact_analysis: {}`)
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	lia := impact.Lookup(mgr)
	if lia == nil {
		t.Fatal("could not find plugin")
	}

	path := "testdata/eopa-bundle.tar.gz"
	bndl, err := loader.NewFileLoader().
		WithSkipBundleVerification(true).
		WithRegoVersion(ast.RegoV0).
		AsBundle(path)
	if err != nil {
		t.Fatalf("eopa bundle: %v", err)
	}
	j := impact.NewJob(ctx, 1, true, bndl, time.Second)
	if err := lia.StartJob(ctx, j); err != nil {
		t.Fatalf("error starting job: %v", err)
	}

	{ // fake a request
		ctx := impact.Enable(logging.NewContext(ctx, &logging.RequestContext{
			ReqPath: "/v1/data/x",
		}), "/v1/data/x")
		ectx := fakeEval{
			body:  ast.MustParseBody("data.x = y"),
			input: ast.Boolean(true),
		}
		exp := ast.NewSet(ast.ObjectTerm(ast.Item(ast.StringTerm("result"), ast.BooleanTerm(true))))
		impact.Enqueue(ctx, &ectx, exp)
	}

	// NOTE(sr): The more time we give the go routines to actually start,
	// the less flaky this test will be, if there are leaked routines.
	time.Sleep(200 * time.Millisecond)
	mgr.Stop(ctx)

	// goleak will assert that no goroutine is still running
}

type fakeEval struct {
	body  ast.Body
	input ast.Value
	ndbc  builtins.NDBCache
}

func (f *fakeEval) CompiledQuery() ast.Body {
	return f.body
}

func (f *fakeEval) ParsedInput() ast.Value {
	return f.input
}

func (f *fakeEval) NDBCache() builtins.NDBCache {
	return f.ndbc
}

func (*fakeEval) Metrics() metrics.Metrics {
	return metrics.New()
}
