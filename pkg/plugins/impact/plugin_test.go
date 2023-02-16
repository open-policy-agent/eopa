package impact_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"

	load_bundle "github.com/styrainc/load-private/pkg/plugins/bundle"
	"github.com/styrainc/load-private/pkg/plugins/discovery"
	"github.com/styrainc/load-private/pkg/plugins/impact"
	inmem "github.com/styrainc/load-private/pkg/storage"
)

func TestStop(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
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

	path := "testdata/load-bundle.tar.gz"
	bndls, err := (&load_bundle.CustomLoader{}).Load(ctx, metrics.New(), []string{path})
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	j := impact.NewJob(ctx, 1, true, bndls[path], time.Second)
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

func pluginMgr(t *testing.T, config string) *plugins.Manager {
	t.Helper()
	h := topdown.NewPrintHook(os.Stderr)
	mux := mux.NewRouter()
	opts := []func(*plugins.Manager){
		plugins.WithRouter(mux),
		plugins.PrintHook(h),
		plugins.EnablePrintStatements(true),
	}
	if !testing.Verbose() {
		opts = append(opts, plugins.Logger(logging.NewNoOpLogger()))
		opts = append(opts, plugins.ConsoleLogger(logging.NewNoOpLogger()))
	}

	store := inmem.New()
	mgr, err := plugins.New([]byte(config), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	disco, err := discovery.New(mgr,
		discovery.Factories(map[string]plugins.Factory{
			impact.Name: impact.Factory(),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}
