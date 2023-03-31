package impact

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/logs"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/server"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown/builtins"

	inmem "github.com/styrainc/load-private/pkg/store"
)

// Impact holds the state of a plugin instantiation
type Impact struct {
	manager *plugins.Manager
	workers *pool.Pool
	ctx     context.Context
	cancel  context.CancelFunc
	log     logging.Logger
	config  Config
	dl      *logs.Plugin
	job     Job
}

func Lookup(manager *plugins.Manager) *Impact {
	if p := manager.Plugin(Name); p != nil {
		return p.(*Impact)
	}
	return nil
}

var mutex = &sync.Mutex{}
var singleton *Impact

type EvalContext interface {
	CompiledQuery() ast.Body
	NDBCache() builtins.NDBCache
	ParsedInput() ast.Value
	Metrics() metrics.Metrics
}

// Enqueue is the quick and dirty entrypoint that the singleton Impact plugin works from
func Enqueue(ctx context.Context, ectx EvalContext, exp ast.Value) {
	if singleton == nil {
		return // plugin not enabled
	}

	if singleton.job == nil {
		singleton.log.Debug("no LIA job")
		return // no LIA job running
	}

	// NOTE(sr): This also serves as a device to stop us from looping infinitely:
	// When we're calling eval below, it's using the singleton's ctx, not the
	// incoming one. Any subsequent calls to impact.Enqueue will thus stop here.
	path := liaEnabled(ctx)
	if path == "" {
		return
	}

	rctx, _ := logging.FromContext(ctx)
	decisionID, _ := logging.DecisionIDFromContext(ctx)

	if !singleton.sample(path, ectx.CompiledQuery()[0]) {
		return
	}

	// TODO(sr): think about this:
	// Go submits a task to be run in the pool. If all goroutines in the pool
	// are busy, a call to Go() will block until the task can be started.
	singleton.workers.Go(func() {
		// NOTE(sr): We're using a new context here because the one we're given is
		// scoped to the HTTP request. Once that's done, it'll be canceled, and that
		// may not give us enough time for the secondary evaluation.
		ctx, cancel := context.WithTimeout(singleton.ctx, 5*time.Second)
		defer cancel()
		ctx = logging.WithDecisionID(ctx, decisionID)
		if err := singleton.eval(ctx, ectx, rctx, dropDataPrefix(path), exp); err != nil {
			singleton.log.Warn("live impact analysis: %v", err)
		}
	})
}

func (i *Impact) sample(reqPath string, query *ast.Expr) bool {
	// NOTE(sr): We have to use rctx.Path AND query for selecting the sample population:
	// otherwise, we'll also run LIA for DL drop/mask decisions and whatnot.
	q, ok := query.Operand(0).Value.(ast.Ref)
	if !ok {
		return false
	}
	requestPath, ok := storage.ParsePath(reqPath)
	if !ok {
		return false
	}
	path, err := storage.NewPathForRef(q)
	if err != nil {
		return false
	}
	if !requestPath[2:].Equal(path) { // this won't be the case for DL drop/mask decisions
		return false
	}
	return rand.Float32() <= i.job.SampleRate()
}

func (i *Impact) Start(ctx context.Context) error {
	i.manager.GetRouter().Handle(httpPrefix, i)

	// TODO(sr): think, benchmark, guess MaxGoroutines?
	i.workers = pool.New().WithMaxGoroutines(runtime.NumCPU())

	// i.cancel is used to abort all running evals in Stop()
	i.ctx, i.cancel = context.WithCancel(ctx)

	mutex.Lock()
	singleton = i
	mutex.Unlock()

	i.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (i *Impact) Stop(context.Context) {
	i.cancel()
	i.workers.Wait()
	i.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
}

func (i *Impact) Reconfigure(context.Context, any) {
	// TODO
}

// We'll evaluate at the configured sampling rate, with a different set of policies (TBD).
// TODO: cache PrepareForEval?
func (i *Impact) eval(ctx context.Context, ectx EvalContext, rctx *logging.RequestContext, path string, exp ast.Value) error {
	// NOTE(sr): While rego.New() could take much more complex things, we know that the queries
	// we're interested in have been generated from API calls. That allows for some simpler moves
	// here:
	queryT := ectx.CompiledQuery()[0].Operand(0)
	input := ectx.ParsedInput()
	mA := ectx.Metrics()

	store := inmem.New()
	txn := storage.NewTransactionOrDie(ctx, store, storage.WriteParams) // never fails, the store is new
	mB := metrics.New()
	opts := []func(*rego.Rego){
		rego.ParsedQuery(ast.NewBody(ast.NewExpr(queryT))),
		rego.Store(store),
		rego.Transaction(txn),
		rego.ParsedBundle(i.job.Bundle().Etag, i.job.Bundle()), // TODO(sr): Etag appropriate?
		rego.NDBuiltinCache(ectx.NDBCache()),
		rego.Metrics(mB),
	}
	if input != nil {
		opts = append(opts, rego.ParsedInput(input))
	}
	r := rego.New(opts...)
	secondaryResult, err := r.Eval(ctx)
	if err != nil {
		return err
	}

	primaryResult, err := unwrap(exp)
	if err != nil {
		return err
	}

	eq := i.equalResults(ctx, primaryResult, secondaryResult)
	if eq && !i.job.PublishEquals() {
		return nil
	}
	var in any
	if input != nil {
		in0, err := ast.ValueToInterface(input, nil)
		if err != nil {
			return err
		}
		in = in0
	}

	var ndbc any = ectx.NDBCache()
	var secRes any
	if len(secondaryResult) == 1 {
		secRes = secondaryResult[0].Expressions[0].Value
	}
	return i.publish(ctx, rctx, path, &in, &primaryResult, &secRes, &ndbc, mA, mB)
}

func unwrap(exp ast.Value) (any, error) {
	var resultObj *ast.Term
	resultSet := exp.(ast.Set)
	if resultSet.Len() == 0 {
		return nil, nil
	}
	// what follows is unwrapping the one result in `resultSet`, to compare it with `res`
	_ = resultSet.Iter(func(t *ast.Term) error {
		resultObj = t
		return nil
	})
	// rv := query.Operand(1)
	// res0 := resultObj.Get(rv) // Q: why doesn't this work?
	var res0 ast.Value
	_ = resultObj.Value.(ast.Object).Iter(func(_, v *ast.Term) error {
		res0 = v.Value
		return nil
	})
	return ast.ValueToInterface(res0, nil)
}

// TODO(sr): increment some metrics according to the comparison outcomes: diff++, same++
func (i *Impact) equalResults(_ context.Context, primaryResult any, secondaryResult rego.ResultSet) bool {
	switch {
	case primaryResult == nil && len(secondaryResult) > 0: // secondary has result, primary has not
		return false
	case primaryResult != nil && len(secondaryResult) == 0: // primary has result, secondary has not
		return false
	case primaryResult != nil && len(secondaryResult) > 0: // both have results
		resUnwrapped := secondaryResult[0].Expressions[0].Value
		return reflect.DeepEqual(resUnwrapped, primaryResult) // TODO(sr): do better than that
	}
	return true // both empty
}

// TODO(sr): don't let this grow out of hand, flush to controller periodically
func (i *Impact) publish(ctx context.Context, rctx *logging.RequestContext, path string, input, resultA, resultB *any, ndbc *any, mA, mB metrics.Metrics) error {
	decisionID, _ := logging.DecisionIDFromContext(ctx)
	res := Result{
		NodeID:     i.manager.ID,
		Path:       path,
		ValueA:     resultA,
		ValueB:     resultB,
		Input:      input,
		EvalNSA:    uint64(mA.Timer("regovm_eval").Int64()),
		EvalNSB:    uint64(mB.Timer("regovm_eval").Int64()),
		DecisionID: decisionID,
	}
	if rctx != nil {
		res.RequestID = rctx.ReqID
		res.Path = dropDataPrefix(rctx.ReqPath)
	}
	i.job.Result(&res)

	// DL for resultA has already been published by the primary eval path
	return i.dlog(ctx, rctx, input, resultB, ndbc, mB)
}

// dlog emits a decision log if DL is available
func (i *Impact) dlog(ctx context.Context, rctx *logging.RequestContext, input, result *any, ndbc *any, m metrics.Metrics) error {
	if i.dl == nil {
		return nil
	}
	decisionID, _ := logging.DecisionIDFromContext(ctx)
	info := server.Info{
		Results:        result,
		Input:          input,
		NDBuiltinCache: ndbc,
		Timestamp:      time.Now(),
		Metrics:        m,
		DecisionID:     decisionID,
	}
	if rctx != nil {
		info.RequestID = rctx.ReqID
		info.Path = dropDataPrefix(rctx.ReqPath)
	}
	return i.dl.Log(ctx, &info)
}

func (i *Impact) StartJob(ctx context.Context, j Job) error {
	mutex.Lock()
	defer mutex.Unlock()
	if i.job != nil {
		return fmt.Errorf("busy with job %s", i.job.ID())
	}
	j.Start(ctx, func() {
		mutex.Lock()
		i.job = nil
		defer mutex.Unlock()
		i.log.Info("stopped live impact analysis job %s", j.ID())
	})
	i.job = j
	i.log.Info("started live impact analysis job %s", j.ID())
	return nil
}

func dropDataPrefix(p string) string {
	return strings.TrimPrefix(strings.TrimPrefix(p, "/v1/data/"), "/v0/data/")
}
