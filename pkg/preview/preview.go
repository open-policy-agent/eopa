package preview

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/rego"
	opaTypes "github.com/open-policy-agent/opa/server/types"
	"github.com/open-policy-agent/opa/storage"
	"github.com/styrainc/enterprise-opa-private/pkg/preview/types"
)

// PreviewOption is the primary interface supported by the Preview struct,
// defining the main Init method which is passed the Preview struct so that
// the struct can register itself with other preview lifecycle hooks.
type PreviewOption interface {
	Init(*Preview) error
}

// StoragePreparer is an interface representing a mutation on the PreviewStorage
// instance, allowing options to modify its setup.
type StoragePreparer interface {
	PrepareStorage(*PreviewStorage) error
}

// CompilerPreparer is an interface representing a mutation on the Compiler instance,
// allowing options to modify its setup.
type CompilerPreparer interface {
	PrepareCompiler(*ast.Compiler) error
}

// RegoOptionProvider is an interface allowing options to define Rego object options
// passed when `rego.New()` is called.
type RegoOptionProvider interface {
	RegoOptions() []func(*rego.Rego)
}

// EvaluationOptionProvider is an interface allowing preview options to define any
// evaluation options to send when calling `Eval()` on the Rego instance.
type EvaluationOptionProvider interface {
	EvaluationOptions() []rego.EvalOption
}

// PostEvaluation is a function which will run as a callback immediately following
// the Rego evaluation, but before processing the final results.
type PostEvaluationHook func()

// ResultProvider is an interface which allows preview options to add or modify the
// preview response based on the the Rego eval result.
type ResultProvider interface {
	Result(*Preview, rego.ResultSet, types.PreviewResponseV1) (types.PreviewResponseV1, error)
}

// Preview is a struct which controls the lifecycle of a preview request. It is
// primarily responsible for managing the request lifecycle, relying on
// preview options to control the preview evaluation behavior.
type Preview struct {
	context     context.Context
	input       ast.Value
	storage     *PreviewStorage
	compiler    *ast.Compiler
	metrics     metrics.Metrics
	transaction storage.Transaction

	previewOptions            []PreviewOption
	storePreparers            []StoragePreparer
	compilerPreparers         []CompilerPreparer
	regoOptionProviders       []RegoOptionProvider
	evaluationOptionProviders []EvaluationOptionProvider
	postEvaluation            []PostEvaluationHook
	resultsProviders          []ResultProvider
}

// NewPreview creates a new Preview struct with the provided context and a new
// preview store and compiler.
func NewPreview(ctx context.Context) *Preview {
	return &Preview{
		context:  ctx,
		storage:  NewPreviewStorage(),
		compiler: ast.NewCompiler(),
	}
}

// WithMetrics overrides the stored metrics.Metrics, returning the preview struct
// for chaining.
func (p *Preview) WithMetrics(m metrics.Metrics) *Preview {
	p.metrics = m
	return p
}

// WithInput overrides the stored input value, returning the preview struct for chaining.
func (p *Preview) WithInput(input ast.Value) *Preview {
	p.input = input
	return p
}

// WithOption registers a new PreviewOption so it can modify the behavior of the preview
// eval during the lifecycle. It returns the preview struct for chaining.
func (p *Preview) WithOption(option PreviewOption) *Preview {
	p.previewOptions = append(p.previewOptions, option)
	return p
}

// WithOptions takes a variable number of PreviewOptions registering each. The preview
// struct is returned for chaining.
func (p *Preview) WithOptions(options ...PreviewOption) *Preview {
	for _, option := range options {
		p.WithOption(option)
	}

	return p
}

// WithStoragePreparer registers a struct matching the StoragePreparer interface allowing
// it to modify the PreviewStore prior to evaluating the preview request. The preview struct
// is returned for chaining.
func (p *Preview) WithStoragePreparer(preparer StoragePreparer) *Preview {
	p.storePreparers = append(p.storePreparers, preparer)
	return p
}

// WithCompilerPreparer registers a struct matching the CompilerPreparer interface allowing
// it to modify the preview compiler prior to evaluating the preview request. The preview struct
// is returned for chaining.
func (p *Preview) WithCompilerPreparer(preparer CompilerPreparer) *Preview {
	p.compilerPreparers = append(p.compilerPreparers, preparer)
	return p
}

// WithRegoOptionProvider registers a struct matching the RegoOptionProvider interface allowing
// it to supply additional options passed when calling `rego.New()`. The preview struct is
// returned for chaining.
func (p *Preview) WithRegoOptionProvider(provider RegoOptionProvider) *Preview {
	p.regoOptionProviders = append(p.regoOptionProviders, provider)
	return p
}

// WithEvaluationOptionProvider registers a struct matching the EvaluationOptionProvider interface
// allowing it to supply additional options passed when calling `Eval()` on the Rego instance.
// The preview struct is returned for chaining.
func (p *Preview) WithEvaluationOptionProvider(provider EvaluationOptionProvider) *Preview {
	p.evaluationOptionProviders = append(p.evaluationOptionProviders, provider)
	return p
}

// WithPostEvalHook registers a function matching PostEvaluation function type allowing it to
// run code after the Rego evaluation is complete but before creating the final response.
func (p *Preview) WithPostEvalHook(hook PostEvaluationHook) *Preview {
	p.postEvaluation = append(p.postEvaluation, hook)
	return p
}

// WithResultProvider registers a struct matching the WithResultProvider interface allowing it to
// modify the final response based on the result of either internal state or the Rego eval result.
func (p *Preview) WithResultProvider(provider ResultProvider) *Preview {
	p.resultsProviders = append(p.resultsProviders, provider)
	return p
}

// Context returns the currently stored preview context.
func (p *Preview) Context() context.Context {
	return p.context
}

// Store returns the stored PreviewStorage struct.
func (p *Preview) Store() *PreviewStorage {
	return p.storage
}

// Metrics returns the stored metrics.Metrics.
func (p *Preview) Metrics() metrics.Metrics {
	return p.metrics
}

// Transaction returns the stored storage.Transaction.
func (p *Preview) Transaction() storage.Transaction {
	return p.transaction
}

// Eval runs the preview evaluation. It does this by running through the Preview
// lifecycle, initializing all registered options, then setting up the preview storage
// and compiler, generating a Rego struct, and then running an Eval on it. When
// complete, it compiles the final preview response using the registered results providers.
func (p *Preview) Eval() (types.PreviewResponseV1, error) {
	result := types.PreviewResponseV1{
		DataResponseV1: opaTypes.DataResponseV1{},
	}

	// Init
	for _, optionProvider := range p.previewOptions {
		err := optionProvider.Init(p)
		if err != nil {
			return result, fmt.Errorf("initializing preview: %w", err)
		}
	}

	// PrepareStore
	for _, preparer := range p.storePreparers {
		err := preparer.PrepareStorage(p.storage)
		if err != nil {
			return result, fmt.Errorf("preparing preview state: %w", err)
		}
	}

	// Create Transaction
	txn, err := p.storage.NewTransaction(p.context, storage.TransactionParams{Context: storage.NewContext().WithMetrics(p.metrics)})
	if err != nil {
		return result, fmt.Errorf("creating transaction: %w", err)
	}
	p.transaction = txn
	defer p.storage.Abort(p.context, txn)

	// PrepareCompiler
	for _, preparer := range p.compilerPreparers {
		err := preparer.PrepareCompiler(p.compiler)
		if err != nil {
			return result, fmt.Errorf("preparing preview compiler: %w", err)
		}
	}

	// RegoOptions
	opts := []func(*rego.Rego){
		rego.Store(p.storage),
		rego.Transaction(p.transaction),
		rego.Compiler(p.compiler),
		rego.ParsedInput(p.input),
		rego.Metrics(p.metrics),
	}
	for _, provider := range p.regoOptionProviders {
		opts = append(opts, provider.RegoOptions()...)
	}
	opaRego := rego.New(opts...)

	// EvaluationOptions
	evalOpts := []rego.EvalOption{
		rego.EvalTransaction(p.transaction),
		rego.EvalParsedInput(p.input),
		rego.EvalMetrics(p.metrics),
	}
	for _, provider := range p.evaluationOptionProviders {
		evalOpts = append(evalOpts, provider.EvaluationOptions()...)
	}

	// Evaluation
	preparedQuery, err := opaRego.PrepareForEval(p.context)
	if err != nil {
		return result, err
	}
	queryResult, err := preparedQuery.Eval(p.context, evalOpts...)
	if err != nil {
		return result, err
	}

	// PostEvaluation
	for _, hooked := range p.postEvaluation {
		hooked()
	}

	// Result
	for _, provider := range p.resultsProviders {
		rs, err := provider.Result(p, queryResult, result)
		if err != nil {
			return result, fmt.Errorf("preparing preview result: %w", err)
		}
		result = rs
	}

	return result, nil
}
