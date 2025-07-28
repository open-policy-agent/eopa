package preview

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/open-policy-agent/eopa/pkg/json"
	"github.com/open-policy-agent/eopa/pkg/preview/types"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/rego"
	opaTypes "github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/topdown/print"
	"github.com/open-policy-agent/opa/v1/version"
)

// WASMResolverOpts is a preview option which adds any defined WASM resolvers to
// the Rego object
type WASMResolversOpt struct {
	manager *plugins.Manager
}

// NewWASMResolversOpt creates a new WASMResolversOpt struct
func NewWASMResolversOpt(manager *plugins.Manager) *WASMResolversOpt {
	return &WASMResolversOpt{
		manager: manager,
	}
}

// Init registers the WASMResolversOpt with the preview struct as a
// RegoOptionProvider
func (w *WASMResolversOpt) Init(preview *Preview) error {
	preview.WithRegoOptionProvider(w)
	return nil
}

// RegoOptions pulls any WASM resolvers from the plugin manager and adds
// them to the Rego options so they are taken into account when processing
// the preview request.
func (w *WASMResolversOpt) RegoOptions() []func(*rego.Rego) {
	// Set resolvers on the base Rego object to avoid having them get
	// re-initialized, and to propagate them to the prepared query.
	resolvers := w.manager.GetWasmResolvers()
	opts := make([]func(*rego.Rego), 0, len(resolvers))
	for _, r := range resolvers {
		for _, entrypoint := range r.Entrypoints() {
			opts = append(opts, rego.Resolver(entrypoint, r))
		}
	}
	return opts
}

// EnvironmentOpts is a preview option which sets up the overall environment in which
// the preview request is run. This includes ensuring any user provided modules and data
// as well as already existing modules and data are available when running preview.
type EnvironmentOpt struct {
	sandbox       bool
	manager       *plugins.Manager
	extraPolicies map[string]string
	rawExtraData  []byte
	extraData     json.Json
}

// NewEnvironmentOpt creates a new EnvironmentOpt struct to set up the preview environment
func NewEnvironmentOpt(sandbox bool, manager *plugins.Manager, extraPolicies map[string]string, extraData []byte) *EnvironmentOpt {
	return &EnvironmentOpt{
		sandbox:       sandbox,
		manager:       manager,
		extraPolicies: extraPolicies,
		rawExtraData:  extraData,
	}
}

// Init decodes the raw data if present for use in the PreviewStorage struct before
// registering the EnvironmentOpt as a StoragePreparer and CompilerPreparer.
func (e *EnvironmentOpt) Init(preview *Preview) error {
	// Parse any extra data
	if e.rawExtraData != nil {
		data, err := json.NewDecoder(bytes.NewReader(e.rawExtraData)).Decode()
		if err != nil {
			return err
		}
		e.extraData = data
	}

	// Register lifecycle hooks
	preview.WithStoragePreparer(e).WithCompilerPreparer(e).WithRegoOptionProvider(e)
	return nil
}

// PrepareStorage adds any user provided policies and data to the
// PreviewStorage. If not in sandbox mode, it also adds the primary
// store so that existing data is available.
func (e *EnvironmentOpt) PrepareStorage(store *PreviewStorage) error {
	if !e.sandbox {
		store.WithPrimaryStorage(e.manager.Store)
	}
	if e.extraData != nil {
		store.WithPreviewData(e.extraData)
	}
	return nil
}

// PrepareCompiler side loads existing policies into the preview compiler. If
// sandbox mode is specified, this is skipped.
func (e *EnvironmentOpt) PrepareCompiler(compiler *ast.Compiler) error {
	if e.sandbox {
		return nil
	}

	primeCompiler := e.manager.GetCompiler()
	if primeCompiler != nil {
		compiler.WithModuleLoader(func(resolved map[string]*ast.Module) (map[string]*ast.Module, error) {
			toLoad := make(map[string]*ast.Module, len(primeCompiler.Modules))
			// Load any compiled modules from the prime compiler that are not defined as part of the preview
			// TODO: This operation could potentially become more efficient if we only loaded policies
			// required to evaluate the request. See https://styrainc.atlassian.net/browse/EOPA-87 for more
			// details on accomplishing this.
			for location, module := range primeCompiler.Modules {
				if _, present := resolved[location]; !present {
					toLoad[location] = module
				}
			}
			return toLoad, nil
		})
	}
	// If no extra policies were sent, run the compile step ourselves to ensure the side loaded
	// modules are populated into the compiler
	if len(e.extraPolicies) == 0 {
		if compiler.Compile(nil); compiler.Failed() {
			return compiler.Errors
		}
	}
	return nil
}

// RegoOptions adds any preview policies sent into the rego object, which will compile them
// when the query is prepared.
func (e *EnvironmentOpt) RegoOptions() []func(*rego.Rego) {
	options := make([]func(*rego.Rego), 0, len(e.extraPolicies))
	for name, policy := range e.extraPolicies {
		options = append(options, rego.Module(name, policy))
	}
	return options
}

// QueryOpt is a preview option which injects the correct query into the preview query.
// This is either a specific string query, or if no specific query was sent, it runs
// the query based on the URL path.
type QueryOpt struct {
	ref   ast.Ref
	query string
}

// NewQueryOpt creates a new QueryOpt struct to inject the correct preview query.
func NewQueryOpt(path string, query string) *QueryOpt {
	return &QueryOpt{
		ref:   stringPathToDataRef(path),
		query: query,
	}
}

// Init registers the QueryOpt with the preview struct as both a rego option
// provider and a result provider
func (q *QueryOpt) Init(preview *Preview) error {
	preview.
		WithRegoOptionProvider(q).
		WithResultProvider(q)
	return nil
}

// RegoOptions sets up the query. If a specific query is provided, it set this
// up to run in the context of the package represented by the path. If not, the
// path reference is queried directly.
func (q *QueryOpt) RegoOptions() []func(*rego.Rego) {
	pkg := q.ref.String()
	if q.query == "" {
		return []func(*rego.Rego){
			rego.Query(pkg),
		}
	}
	// query packages should not have the default root document
	pkg = strings.TrimPrefix(pkg, ast.DefaultRootDocument.Value.String()+".")
	return []func(*rego.Rego){
		rego.Query(q.query),
		// rego.Imports([]string{}), // TODO: Do we need to parse this out...?
		rego.Package(pkg),
	}
}

// Results is responsible for mixing the query result into the response. If a
// specific query was sent, it provides the full result expression value.
// Otherwise it extracts the result, returning it without the extra context.
//
// Switching the result format in this way makes the return compatible with the
// DAS data API.
func (q *QueryOpt) Result(_ *Preview, results rego.ResultSet, response types.PreviewResponseV1) (types.PreviewResponseV1, error) {
	if q.query == "" && results != nil {
		response.Result = &results[0].Expressions[0].Value
	} else if results != nil {
		itfc := func() any { return results[0] }()
		response.Result = &itfc
	}
	return response, nil
}

// BasicOpt is a preview option that holds multiple boolean-type simple options
// together so each does not need a separate option struct.
type BasicOpt struct {
	strictCompile       bool
	metrics             bool
	instrumentation     bool
	strictBuiltinErrors bool
}

// NewBasicOpt creates a new BasicOpt struct holding several simple boolean
// preview options.
func NewBasicOpt(strictCompile, metrics, instrumentation, strictBuiltinErrors bool) *BasicOpt {
	return &BasicOpt{
		strictCompile:       strictCompile,
		metrics:             metrics,
		instrumentation:     instrumentation,
		strictBuiltinErrors: strictBuiltinErrors,
	}
}

// Init registers the BasicOpt struct with the preview struct as a compiler
// preparer, rego option provider, evaluation option provider, and a result
// provider.
func (b *BasicOpt) Init(preview *Preview) error {
	preview.
		WithCompilerPreparer(b).
		WithRegoOptionProvider(b).
		WithEvaluationOptionProvider(b).
		WithResultProvider(b)

	return nil
}

// PrepareCompiler sets the compiler to run in strict mode if the strict
// option is set.
func (b *BasicOpt) PrepareCompiler(compiler *ast.Compiler) error {
	compiler.WithStrict(b.strictCompile)
	return nil
}

// RegoOptions configures the instrumentation setting and sets the strict
// builtin errors settings for the Rego evaluation.
func (b *BasicOpt) RegoOptions() []func(*rego.Rego) {
	return []func(*rego.Rego){
		rego.Instrument(b.instrumentation),
		rego.StrictBuiltinErrors(b.strictBuiltinErrors),
	}
}

// EvaluationOptions set up the instrumentation setting for the evaluation.
func (b *BasicOpt) EvaluationOptions() []rego.EvalOption {
	return []rego.EvalOption{
		rego.EvalInstrument(b.instrumentation),
	}
}

// Result checks the metrics and instrumentation setting and if true adds the
// metrics information to the preview response.
func (b *BasicOpt) Result(preview *Preview, _ rego.ResultSet, response types.PreviewResponseV1) (types.PreviewResponseV1, error) {
	if b.metrics || b.instrumentation {
		response.Metrics = preview.metrics.All()
	}
	return response, nil
}

// NDBuiltinCacheOpt injects any values sent to the preview API from provided input,
// allowing various builtin to return predefined results.
type NDBuiltinCacheOpt struct {
	data  map[string]map[string]any
	cache builtins.NDBCache
}

// NewNDBuiltinCacheOpt creates a new NDBuiltinCacheOpt holding the provided cache
// values for use in the preview evaluation.
func NewNDBuiltinCacheOpt(data map[string]map[string]any) *NDBuiltinCacheOpt {
	return &NDBuiltinCacheOpt{
		data: data,
	}
}

// Init parses and sets up the actual NDBCache struct if valid cache data was
// provided. It then registers the NDBuiltinCacheOpt as an evaluation option provider.
func (n *NDBuiltinCacheOpt) Init(preview *Preview) error {
	if n.data != nil {
		n.cache = builtins.NDBCache{}
		for name, vm := range n.data {
			for operands, value := range vm {
				term, err := ast.ParseTerm(operands)
				if err != nil {
					return fmt.Errorf("ND Builtin Cache term parse: %w", err)
				}
				val, err := ast.InterfaceToValue(value)
				if err != nil {
					return fmt.Errorf("ND Builtin Cache interface to value: %w", err)
				}
				n.cache.Put(name, term.Value, val)
			}
		}

		preview.WithEvaluationOptionProvider(n)
	}
	return nil
}

// EvaluationOptions sets the Rego evaluator to use the supplied data as the
// NDBuiltinCache for the Rego evaluation.
func (n *NDBuiltinCacheOpt) EvaluationOptions() []rego.EvalOption {
	return []rego.EvalOption{rego.EvalNDBuiltinCache(n.cache)}
}

// ProvenanceOpt is a preview option which will add provenance data to the response
type ProvenanceOpt struct {
	addProvenance bool
}

// NewProvenanceOpt creates a new ProvenanceOpt for adding provenance data to
// a preview query.
func NewProvenanceOpt(addProvenance bool) *ProvenanceOpt {
	return &ProvenanceOpt{
		addProvenance: addProvenance,
	}
}

// Init ensures addProvenance is true, and if so registers the ProvenanceOpt with
// the preview struct as a result provider.
func (p *ProvenanceOpt) Init(preview *Preview) error {
	if p.addProvenance {
		preview.WithResultProvider(p)
	}
	return nil
}

// Result adds provenance data to the preview response. This gathers revisions from
// the store. This is done at this stage to ensure the environment and transaction
// are fully prepared prior to attempting to gather available revisions.
func (p *ProvenanceOpt) Result(preview *Preview, _ rego.ResultSet, response types.PreviewResponseV1) (types.PreviewResponseV1, error) {
	revisions, err := p.getRevisions(preview.Context(), preview.Store(), preview.Transaction())
	if err != nil {
		return response, err
	}
	response.Provenance = &opaTypes.ProvenanceV1{
		Version:   version.Version,
		Vcs:       version.Vcs,
		Timestamp: version.Timestamp,
		Hostname:  version.Hostname,
	}

	response.Provenance.Bundles = map[string]opaTypes.ProvenanceBundleV1{}
	for name, revision := range revisions {
		response.Provenance.Bundles[name] = opaTypes.ProvenanceBundleV1{Revision: revision}
	}
	return response, nil
}

// getRevisions reads any available revisions from the Store.
func (p *ProvenanceOpt) getRevisions(ctx context.Context, store storage.Store, txn storage.Transaction) (map[string]string, error) {
	revisions := map[string]string{}

	// read all bundle revisions from storage (if any exist)
	names, err := bundle.ReadBundleNamesFromStore(ctx, store, txn)
	if err != nil && !storage.IsNotFound(err) {
		return revisions, err
	}

	for _, name := range names {
		r, err := bundle.ReadBundleRevisionFromStore(ctx, store, txn, name)
		if err != nil && !storage.IsNotFound(err) {
			return revisions, err
		}
		revisions[name] = r
	}

	return revisions, nil
}

// PrintOpt is a preview option which collects and adds any Rego print() statements
// from the preview query to the response.
type PrintOpt struct {
	doPrint bool
	printed *strings.Builder
}

// NewPrintOpt creates a new PrintOpt struct ready for use in a preview request
func NewPrintOpt(doPrint bool) *PrintOpt {
	return &PrintOpt{
		doPrint: doPrint,
	}
}

// Init is a noop if doPrint if off, otherwise it sets up a string builder for
// collecting printed output and registers itself as a preview compiler
// preparer, rego options provider, and result provider.
func (p *PrintOpt) Init(preview *Preview) error {
	if !p.doPrint {
		return nil
	}

	p.printed = new(strings.Builder)

	preview.
		WithCompilerPreparer(p).
		WithRegoOptionProvider(p).
		WithResultProvider(p)

	return nil
}

// PrepareCompiler set the compiler to enable print statement when the Rego is executed.
func (p *PrintOpt) PrepareCompiler(compiler *ast.Compiler) error {
	compiler.WithEnablePrintStatements(true)
	return nil
}

// RegoOptions sets up the PrintOpt as a print hook in the Rego struct.
func (p *PrintOpt) RegoOptions() []func(*rego.Rego) {
	return []func(*rego.Rego){rego.PrintHook(p)}
}

// Result adds any printed string to the preview response.
func (p *PrintOpt) Result(_ *Preview, _ rego.ResultSet, response types.PreviewResponseV1) (types.PreviewResponseV1, error) {
	response.Printed = p.printed.String()
	return response, nil
}

// Print matches the Rego PrintHook interface, and collects any printed strings
// into the strings builder.
func (p PrintOpt) Print(_ print.Context, msg string) error {
	p.printed.WriteString(msg)
	p.printed.WriteByte('\n')
	return nil
}

// stringPathToDataRef is duplicated from OPA to turn a query path into an
// AST data reference coming from the DefaultRootDocument.
func stringPathToDataRef(s string) (r ast.Ref) {
	result := ast.Ref{ast.DefaultRootDocument}
	return append(result, stringPathToRef(s)...)
}

// stringPathToRef is duplicated from OPA and will split a path string into
// parts, parsing them and returning each as a AST reference part.
func stringPathToRef(s string) (r ast.Ref) {
	if len(s) == 0 {
		return r
	}
	p := strings.Split(s, "/")
	for _, x := range p {
		if x == "" {
			continue
		}
		if y, err := url.PathUnescape(x); err == nil {
			x = y
		}
		i, err := strconv.Atoi(x)
		if err != nil {
			r = append(r, ast.StringTerm(x))
		} else {
			r = append(r, ast.IntNumberTerm(i))
		}
	}
	return r
}
