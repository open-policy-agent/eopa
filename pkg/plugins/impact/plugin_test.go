package impact_test

import (
	"context"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/styrainc/load-private/pkg/plugins/impact"
	inmem "github.com/styrainc/load-private/pkg/store"
	"go.uber.org/goleak"
)

func TestStop(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	mgr := getTestManager()
	config := `
sampling_rate: 1
bundle_path: testdata/load-bundle.tar.gz
`
	c, err := impact.Factory().Validate(mgr, []byte(config))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	dp := impact.Factory().New(mgr, c)
	ctx := context.Background()
	if err := dp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// fake a request
	ctx = logging.NewContext(ctx, &logging.RequestContext{
		ReqPath: "/v1/data/x",
	})
	ectx := fakeEval{
		body:  ast.MustParseBody("data.x = y"),
		input: ast.Boolean(true),
	}
	exp := ast.NewSet(ast.ObjectTerm(ast.Item(ast.StringTerm("result"), ast.BooleanTerm(true))))
	impact.Enqueue(ctx, &ectx, exp)

	// NOTE(sr): The more time we give the go routines to actually start,
	// the less flaky this test will be, if there are leaked routines.
	time.Sleep(200 * time.Millisecond)
	dp.Stop(ctx)

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

func getTestManager() *plugins.Manager {
	return getTestManagerWithOpts(nil)
}

func getTestManagerWithOpts(config []byte, stores ...storage.Store) *plugins.Manager {
	store := inmem.New()
	if len(stores) == 1 {
		store = stores[0]
	}

	manager, err := plugins.New(config, "test-instance-id", store)
	if err != nil {
		panic(err)
	}
	return manager
}
