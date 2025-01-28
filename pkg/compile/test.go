package compile

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/tester"
	"github.com/open-policy-agent/opa/v1/topdown"
)

type config struct {
	unknowns    []string
	constraints *ConstraintSet
}

// TODO(sr): Is this crude way of treating metadata sufficient? Or do we need to
// carry along the association to rules...?
// TODO(sr): I think this is cachable -- in a single test run, the module won't
// change.
func configFromMetadata(mod *ast.Module) (*config, error) {
	c := config{}
	modulesList := []*ast.Module{mod}
	as, errs := ast.BuildAnnotationSet(modulesList)
	if len(errs) > 0 {
		return nil, errs
	}
	var cs []*Constraint
	for _, ref := range as.Flatten() {
		if ann := ref.Annotations; ann != nil && len(ann.Custom) > 0 {
			if unk, ok := ann.Custom["unknowns"]; ok {
				if unk, ok := unk.([]any); ok {
					for i := range unk {
						if s, ok := unk[i].(string); ok {
							c.unknowns = append(c.unknowns, s)
						}
					}
				}
			}
			if tgts, ok := ann.Custom["targets"]; ok {
				if tgts, ok := tgts.(map[string]any); ok {
					for target, dialects := range tgts {
						if dialects, ok := dialects.([]any); ok {
							for i := range dialects {
								if dialect, ok := dialects[i].(string); ok {
									c, err := NewConstraints(target, dialect)
									if err != nil {
										return nil, err
									}
									cs = append(cs, c)
								}
							}
						}
					}
				}
			}
		}
	}
	c.constraints = NewConstraintSet(cs...)

	return &c, nil
}

func RunDataPolicyTest(ctx context.Context, rctx *tester.RunnerCtx, mod *ast.Module, rule *ast.Rule) bool {
	ruleName := ruleName(rule.Head)

	// find unknowns from metadata
	config, err := configFromMetadata(mod)
	if err != nil {
		panic(err) // TODO. Obviously.
	}
	if len(config.unknowns) == 0 || strings.HasPrefix(ruleName, tester.SkipTestPrefix) {
		return false
	}

	printbuf := bytes.NewBuffer(nil)
	var builtinErrors []topdown.Error
	rg := rego.New(
		rego.Store(rctx.Store),
		rego.Transaction(rctx.Txn),
		rego.Compiler(rctx.Compiler),
		rego.Query(rule.Path().String()),
		rego.Runtime(rctx.Runtime),
		rego.PrintHook(topdown.NewPrintHook(printbuf)),
		rego.BuiltinErrorList(&builtinErrors),
		rego.Unknowns(config.unknowns),
	)
	// NOTE(sr): maybe filter tagged tests (MD, too)

	// Register custom builtins on rego instance
	for _, v := range rctx.CustomBuiltins {
		v.Func(rg)
	}

	t0 := time.Now()
	pq, err := rg.Partial(ctx)
	dt := time.Since(t0)

	tr := newResult(rule.Loc(), mod.Package.Path.String(), rule.Head.Ref().String(), dt, nil, printbuf.Bytes())

	// If there was an error other than errors from builtins, prefer that error.
	if err != nil {
		tr.Error = err
	} else if rctx.RaiseBuiltinErrors && len(builtinErrors) > 0 {
		if len(builtinErrors) == 1 {
			tr.Error = &builtinErrors[0]
		} else {
			tr.Error = fmt.Errorf("%v", builtinErrors)
		}
	}

	// give up if the query is empty: nothing to check
	if len(pq.Queries) == 0 || len(pq.Queries[0]) == 0 {
		return false
	}

	// We've got one extra layer of indirection here: due to the "with" in our tests,
	// the queries always ref a partial support module. That module contains the "real"
	// queries we're worrying about.
	// As an approximation, we'll unwrap this -- what isn't taken into account is extra
	// support rules, but so far, those have not mattered for the post-PE analysis.

	// first, find the rule under test:
	// assumption: the first ref is what we want.
	// So even if you do
	//     test_foo if data.fox.bar with data.xyz as true
	// the ref we select is data.fox.bar
	// TODO(sr): consider negative filter tests, "not include with input.user as ..."

	var tgt ast.Ref
	expr := pq.Queries[0][0]
	switch {
	case expr.IsCall():
		tgt = expr.Operand(0).Value.(ast.Ref)
	default:
		return false // no analysis, give up
	}

	// second, we select the PE queries from the support modules' rules' bodies
	// assumption: PE support modules in our context don't fuzz with ref heads
	// (so we use Head.Name here to find the queries)
	needle := ast.Var(tgt[len(tgt)-1].Value.(ast.String))
	qs := []ast.Body{}
	for i := range pq.Support {
		for j := range pq.Support[i].Rules {
			r := pq.Support[i].Rules[j]
			if r.Head.Name.Equal(needle) {
				if !r.Body.Equal(constantTrueBody) {
					qs = append(qs, r.Body)
				}
			}
		}
	}

	// finally, we run the compile checks against our synthetic PE result
	pq0 := &rego.PartialQueries{
		Queries: qs,
		Support: pq.Support,
	}

	if res := Check(pq0, config.constraints).ASTErrors(); len(res) > 0 {
		tr.Error = ast.Errors(res)
		rctx.Results <- tr
	}

	return false
}

var constantTrueBody = ast.NewBody(ast.NewExpr(ast.BooleanTerm(true)))

// Stuff we had to copy from OPA

func newResult(loc *ast.Location, pkg, name string, duration time.Duration, trace []*topdown.Event, output []byte) *tester.Result {
	return &tester.Result{
		Location: loc,
		Package:  pkg,
		Name:     name,
		Duration: duration,
		Trace:    trace,
		Output:   output,
	}
}

func ruleName(h *ast.Head) string {
	ref := h.Ref()
	switch last := ref[len(ref)-1].Value.(type) {
	case ast.Var:
		return string(last)
	case ast.String:
		return string(last)
	default:
		return ""
	}
}

func WrapPrettyReporter(rep tester.Reporter) tester.Reporter {
	return &wrapper{wrappee: rep}
}

type wrapper struct {
	wrappee tester.Reporter
}

func (w *wrapper) Report(ch chan *tester.Result) error {
	wrappeeCh := make(chan *tester.Result)
	var captured []*tester.Result
	go func() {
		for tr := range ch {
			if tr.Error != nil {
				if errs, ok := tr.Error.(ast.Errors); ok && errs[0].Code == code {
					captured = append(captured, tr)
					continue // don't pass this one along to our wrappee
				}
			}
			wrappeeCh <- tr
		}
		close(wrappeeCh)
	}()
	if err := w.wrappee.Report(wrappeeCh); err != nil {
		return err
	}

	if len(captured) == 0 {
		return nil
	}

	aggregated := aggregateResults(captured)
	fmt.Println(strings.Repeat("-", 80))
	fmt.Println("Data Policy Analysis:")

	for _, item := range aggregated {
		err := item.Error
		occurrences := item.Occurrences
		n := len(occurrences.Names)
		fmt.Println(err.Error())

		// Display up to 3 occurrences, but if there is only one more, don't display
		// "and 1 more occurrances", just print one more.
		k := min(4, n)
		if n >= 5 {
			k = 3
		}
		for i := range k {
			fmt.Printf("  %s (%s)\n", occurrences.Names[i], occurrences.Locations[i])
		}
		if n >= 5 {
			fmt.Printf("  and %d more occurrences\n", n-k)
		}
		fmt.Println()
	}
	return nil
}

type errorOccurrence struct {
	Locations []*ast.Location
	Names     []string
}

type errorWithOccurrences struct {
	Error       ast.Error
	Occurrences errorOccurrence
}

func aggregateResults(results []*tester.Result) []errorWithOccurrences {
	aggregated := make(map[ast.Error]errorOccurrence)

	for _, result := range results {
		es := result.Error.(ast.Errors)
		for i := range es {
			e := *es[i]
			occ, ok := aggregated[e]
			if !ok {
				occ = errorOccurrence{
					Locations: make([]*ast.Location, 0),
					Names:     make([]string, 0),
				}
			}
			occ.Locations = append(occ.Locations, result.Location)
			occ.Names = append(occ.Names, result.Name)
			aggregated[e] = occ
		}
	}

	// Convert map to slice, sort it, for stable error output
	sorted := make([]errorWithOccurrences, 0, len(aggregated))
	for err, occ := range aggregated {
		sorted = append(sorted, errorWithOccurrences{
			Error:       err,
			Occurrences: occ,
		})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Error.Location.Compare(sorted[j].Error.Location) == -1
	})

	return sorted
}
