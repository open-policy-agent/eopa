package vm

import (
	"encoding/json"
	"fmt"
	"strconv"
	gostrings "strings"
	"time"

	"github.com/styrainc/enterprise-opa-private/pkg/builtins/rego"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"
	"github.com/open-policy-agent/opa/util"
)

const (
	// regoEvalBuiltinCacheKey is the key in the builtin context cache that
	// points to the rego.eval() specific intra-query cache resides at.
	regoEvalBuiltinCacheKey        regoEvalKey = "REGO_EVAL_CACHE_KEY"
	interQueryCacheDurationDefault             = 60 * time.Second
)

type (
	regoEvalNamespaceContextKey struct{}
	regoEvalOptsContextKey      struct{}

	intraQueryCache struct {
		entries *util.HashMap
	}

	intraQueryCacheEntry struct {
		Executable Executable
		Error      error
	}

	interQueryCacheEntry struct {
		Executable Executable
		ExpiresAt  time.Time
	}

	regoEvalKey string
)

var (
	allowedKeys = ast.NewSet(
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("filename"),
		ast.StringTerm("input"),
		ast.StringTerm("module"),
		ast.StringTerm("path"),
		ast.StringTerm("raise_error"),
		ast.StringTerm("version"),
	)

	requiredKeys = ast.NewSet(ast.StringTerm("module"), ast.StringTerm("path"))
)

func builtinRegoEval(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(allowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameters(s): %v", invalidKeys)
	}

	missingKeys := requiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameters(s): %v", missingKeys)
	}

	path, err := getRequestString(obj, "path")
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
	}

	if p := string(path); p != "" && !gostrings.HasPrefix(p, "data.") {
		path = ast.String("data." + p)
	}

	input := obj.Get(ast.StringTerm("input"))

	raiseError, err := getRequestBoolWithDefault(obj, "raise_error", true)
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
	}

	filename, err := getRequestStringWithDefault(obj, "filename")
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
	}

	module, err := getRequestStringWithDefault(obj, "module")
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
	}

	version, err := getRequestStringWithDefault(obj, "version")
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
	}

	interQueryCacheEnabled, err := getRequestBoolWithDefault(obj, "cache", false)
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
	}

	ttl, err := getRequestTimeoutWithDefault(obj, "cache_duration", interQueryCacheDurationDefault)
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
	}

	key := ast.NewObject(
		[2]*ast.Term{ast.StringTerm("filename"), ast.NewTerm(filename)},
		[2]*ast.Term{ast.StringTerm("module"), ast.NewTerm(module)},
		[2]*ast.Term{ast.StringTerm("version"), ast.NewTerm(version)},
		[2]*ast.Term{ast.StringTerm("path"), ast.NewTerm(path)},
	)

	ref, err := ast.ParseRef(string(path))
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, builtins.NewOperandErr(1, "'%s' must be a reference", "path"))
	}

	sp, err := storage.NewPathForRef(ref)
	if err != nil {
		return handleBuiltinErr(rego.RegoEvalName, bctx.Location, builtins.NewOperandErr(1, "'%s' must be a reference", "path"))
	}

	result, err := func() (ast.Value, error) {
		spath := sp.String()[1:]
		executable, err := compileRego(bctx, filename, module, spath, key, interQueryCacheEnabled, ttl)
		if err != nil {
			return nil, err
		}

		vm := NewVM().
			WithExecutable(executable).
			WithDataNamespace(bctx.Context.Value(regoEvalNamespaceContextKey{}))
		opts := EvalOptsFromContext(bctx.Context)
		if input != nil {
			i, err := ast.JSON(input.Value)
			if err != nil {
				return nil, err
			}

			opts.Input = &i
		}
		return vm.Eval(bctx.Context, spath, opts)
	}()

	if err != nil {
		if !raiseError {
			m := map[string]interface{}{
				"error": map[string]interface{}{
					"message": string(err.Error()),
				},
			}

			result, err = ast.InterfaceToValue(m)
			if err != nil {
				return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
			}
		} else {
			return handleBuiltinErr(rego.RegoEvalName, bctx.Location, err)
		}
	}

	return iter(ast.NewTerm(result))
}

func compileRego(bctx topdown.BuiltinContext, filename ast.String, module ast.String, path string, key ast.Object, interQueryCacheEnabled bool, ttl time.Duration) (Executable, error) {
	executable, ok, err := checkCompilationCaches(bctx, key, interQueryCacheEnabled)
	if err != nil {
		return nil, err
	} else if ok {
		return executable, nil
	}

	parsed, err := ast.ParseModule(string(filename), string(module))
	if err != nil {
		return nil, err
	}

	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/",
				Path:   "/",
				Raw:    []byte(module),
				Parsed: parsed,
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(path)
	if err := compiler.Build(bctx.Context); err != nil {
		return nil, err
	}

	bundle := compiler.Bundle()
	var ir ir.Policy
	if err := json.Unmarshal(bundle.PlanModules[0].Raw, &ir); err != nil {
		return nil, err
	}

	executable, compileErr := NewCompiler().WithPolicy(&ir).Compile()
	if err := insertCaches(bctx, key, executable, compileErr, interQueryCacheEnabled, ttl); err != nil {
		return nil, err
	}

	return executable, compileErr
}

func getRequestString(obj ast.Object, key string) (ast.String, error) {
	if s, ok := obj.Get(ast.StringTerm(key)).Value.(ast.String); ok {
		return s, nil
	}

	return "", builtins.NewOperandErr(1, "'%s' must be string", key)
}

func getRequestStringWithDefault(obj ast.Object, key string) (ast.String, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return ast.String(""), nil
	}

	if s, ok := v.Value.(ast.String); ok {
		return s, nil
	}

	return "", builtins.NewOperandErr(1, "'%s' must be string", key)
}

func getRequestBoolWithDefault(obj ast.Object, key string, def bool) (bool, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return def, nil
	}

	if b, ok := v.Value.(ast.Boolean); ok {
		return bool(b), nil
	}

	return false, builtins.NewOperandErr(1, "'%s' must be bool", key)
}

func getRequestTimeoutWithDefault(obj ast.Object, key string, def time.Duration) (time.Duration, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return def, nil
	}

	var timeout time.Duration
	switch t := v.Value.(type) {
	case ast.Number:
		timeoutInt, ok := t.Int64()
		if !ok {
			return timeout, fmt.Errorf("invalid timeout number value %v, must be int64", v)
		}
		return time.Duration(timeoutInt), nil

	case ast.String:
		// Support strings without a unit, treat them the same as just a number value (ns)
		var err error
		timeoutInt, err := strconv.ParseInt(string(t), 10, 64)
		if err == nil {
			return time.Duration(timeoutInt), nil
		}

		// Try parsing it as a duration (requires a supported units suffix)
		timeout, err = time.ParseDuration(string(t))
		if err != nil {
			return timeout, fmt.Errorf("invalid timeout value %v: %s", v, err)
		}
		return timeout, nil

	default:
		return timeout, builtins.NewOperandErr(1, "'timeout' must be one of {string, number} but got %s", ast.TypeName(t))
	}
}

func checkCompilationCaches(bctx topdown.BuiltinContext, key ast.Object, interQueryCacheEnabled bool) (Executable, bool, error) {
	if interQueryCacheEnabled {
		if executable, ok, err := checkInterQueryCache(bctx, key); ok {
			return executable, true, err
		}
	}

	return checkIntraQueryCache(bctx, key)
}

func checkInterQueryCache(bctx topdown.BuiltinContext, key ast.Object) (Executable, bool, error) {
	cache := bctx.InterQueryBuiltinCache

	// TODO: Cache keys will not overlap with the http.send or
	// sql.send cache keys because each builtins has unique
	// required keys in their request objects. This is definitely
	// not an ideal arrangement to guarantee the isolation between
	// all the builtins.

	serializedResp, found := cache.Get(key)
	if !found {
		return nil, false, nil
	}

	resp := serializedResp.(*interQueryCacheEntry)

	if getCurrentTime(bctx).Before(resp.ExpiresAt) {
		bctx.Metrics.Counter(rego.RegoEvalInterQueryCacheHits).Incr()
		return resp.Executable, true, nil
	}

	// No valid entry found.

	return nil, false, nil
}

func checkIntraQueryCache(bctx topdown.BuiltinContext, req ast.Object) (Executable, bool, error) {
	if v := getIntraQueryCache(bctx).Get(req); v != nil {
		bctx.Metrics.Counter(rego.RegoEvalIntraQueryCacheHits).Incr()
		return v.Executable, true, v.Error
	}

	return nil, false, nil
}

func getIntraQueryCache(bctx topdown.BuiltinContext) *intraQueryCache {
	raw, ok := bctx.Cache.Get(regoEvalBuiltinCacheKey)
	if !ok {
		c := newIntraQueryCache()
		bctx.Cache.Put(regoEvalBuiltinCacheKey, c)
		return c
	}

	return raw.(*intraQueryCache)
}

func newInterQueryCacheEntry(bctx topdown.BuiltinContext, executable Executable, ttl time.Duration) *interQueryCacheEntry {
	return &interQueryCacheEntry{
		Executable: executable,
		ExpiresAt:  getCurrentTime(bctx).Add(ttl),
	}
}

func (e interQueryCacheEntry) SizeInBytes() int64 {
	return int64(len(e.Executable))
}

func (e interQueryCacheEntry) Clone() (cache.InterQueryCacheValue, error) {
	return interQueryCacheEntry{Executable: e.Executable, ExpiresAt: e.ExpiresAt}, nil
}

func newIntraQueryCache() *intraQueryCache {
	return &intraQueryCache{
		entries: util.NewHashMap(
			func(k1, k2 util.T) bool {
				return k1.(ast.Value).Compare(k2.(ast.Value)) == 0
			},
			func(k util.T) int {
				return k.(ast.Value).Hash()
			}),
	}
}

func (cache *intraQueryCache) Get(key ast.Value) *intraQueryCacheEntry {
	if v, ok := cache.entries.Get(key); ok {
		v := v.(intraQueryCacheEntry)
		return &v
	}

	return nil
}

func (cache *intraQueryCache) PutResponse(key ast.Value, executable Executable) {
	cache.entries.Put(key, intraQueryCacheEntry{Executable: executable})
}

func (cache *intraQueryCache) PutError(key ast.Value, err error) {
	cache.entries.Put(key, intraQueryCacheEntry{Error: err})
}

func insertCaches(bctx topdown.BuiltinContext, req ast.Object, resp Executable, queryErr error, interQueryCacheEnabled bool, ttl time.Duration) error {
	if queryErr == nil && interQueryCacheEnabled {
		// TOD: Only cache successful queries for across
		// queries; currently we can't separate between
		// transient errors (e.g., network issues) and
		// persistent errors (e.g., query syntax). Hence, it's
		// impossible to know when queries actually warrant
		// for retries and should not be cached.
		if err := insertInterQueryCache(bctx, req, resp, ttl); err != nil {
			return err
		}
	}

	// Within a query we expect deterministic results, hence cache
	// errors too.

	insertIntraQueryCache(bctx, req, resp, queryErr)
	return nil
}

func insertInterQueryCache(bctx topdown.BuiltinContext, req ast.Object, executable Executable, ttl time.Duration) error {
	entry := newInterQueryCacheEntry(bctx, executable, ttl)
	bctx.InterQueryBuiltinCache.Insert(req, entry)
	return nil
}

func insertIntraQueryCache(bctx topdown.BuiltinContext, req ast.Object, executable Executable, queryErr error) {
	if queryErr == nil {
		getIntraQueryCache(bctx).PutResponse(req, executable)
	} else {
		getIntraQueryCache(bctx).PutError(req, queryErr)
	}
}

func getCurrentTime(bctx topdown.BuiltinContext) time.Time {
	var current time.Time

	value, err := ast.JSON(bctx.Time.Value)
	if err != nil {
		return current
	}

	valueNum, ok := value.(json.Number)
	if !ok {
		return current
	}

	valueNumInt, err := valueNum.Int64()
	if err != nil {
		return current
	}

	current = time.Unix(0, valueNumInt).UTC()
	return current
}

func handleBuiltinErr(name string, loc *ast.Location, err error) error {
	switch err := err.(type) {
	case builtins.ErrOperand:
		return &topdown.Error{
			Code:     topdown.TypeErr,
			Message:  fmt.Sprintf("%v: %v", name, err.Error()),
			Location: loc,
		}
	default:
		return &topdown.Error{
			Code:     topdown.BuiltinErr,
			Message:  fmt.Sprintf("%v: %v", name, err.Error()),
			Location: loc,
		}
	}
}

func init() {
	rego.RegisterBuiltinRegoEval(builtinRegoEval)
}
