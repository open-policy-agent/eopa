package builtins

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/topdown/cache"
	"github.com/open-policy-agent/opa/v1/util"
)

// Builtins is the registry of built-in functions supported by Enterprise OPA.
// Call RegisterBuiltin to add a new built-in.
var Builtins []*ast.Builtin
var builtinFunctions = map[string]topdown.BuiltinFunc{}

func RegisterBuiltinFunc(name string, f topdown.BuiltinFunc) {
	builtinFunctions[name] = builtinErrorWrapper(name, f)
}

func builtinErrorWrapper(name string, fn topdown.BuiltinFunc) topdown.BuiltinFunc {
	return func(bctx topdown.BuiltinContext, args []*ast.Term, iter func(*ast.Term) error) error {
		err := fn(bctx, args, iter)
		if err == nil {
			return nil
		}
		return handleBuiltinErr(name, bctx.Location, err)
	}
}

func handleBuiltinErr(name string, loc *ast.Location, err error) error {
	switch err := err.(type) {
	case builtins.ErrOperand:
		e := &topdown.Error{
			Code:     topdown.TypeErr,
			Message:  fmt.Sprintf("%v: %v", name, err.Error()),
			Location: loc,
		}
		return e.Wrap(err)
	default:
		e := &topdown.Error{
			Code:     topdown.BuiltinErr,
			Message:  fmt.Sprintf("%v: %v", name, err.Error()),
			Location: loc,
		}
		return e.Wrap(err)
	}
}

// RegisterBuiltin adds a new built-in function to the registry.
func RegisterBuiltin(b *ast.Builtin) {
	Builtins = append(Builtins, b)
	BuiltinMap[b.Name] = b
	if len(b.Infix) > 0 {
		BuiltinMap[b.Infix] = b
	}
}

// BuiltinMap provides a convenient mapping of built-in names to
// built-in definitions.
var BuiltinMap map[string]*ast.Builtin

// DefaultBuiltins is the registry of built-in functions supported in Enterprise
// OPA by default. When adding a new built-in function to Enterprise OPA, update
// this list.
var DefaultBuiltins = [...]*ast.Builtin{
	// SQL/database builtins.
	dynamoDBGet,
	dynamoDBQuery,
	mongoDBFind,
	mongoDBFindOne,
	regoEval,
	sqlSend,
	vaultSend,
	neo4jQuery,
	redisQuery,
}

func getRequestStringWithDefault(obj ast.Object, key string, def string) (string, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return def, nil
	}

	if s, ok := v.Value.(ast.String); ok {
		return string(s), nil
	}

	return "", builtins.NewOperandErr(1, "'%s' must be string", key)
}

func getRequestObject(obj ast.Object, key string) (ast.Object, error) {
	o := obj.Get(ast.StringTerm(key))
	if o == nil {
		return nil, builtins.NewOperandErr(1, "'%s' missing", key)
	}

	if o, ok := o.Value.(ast.Object); ok {
		return o, nil
	}

	return nil, builtins.NewOperandErr(1, "'%s' must be object", key)
}

func getRequestObjectWithDefault(obj ast.Object, key string, def ast.Object) (ast.Object, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return def, nil
	}

	if o, ok := v.Value.(ast.Object); ok {
		return o, nil
	}

	return nil, builtins.NewOperandErr(1, "'%s' must be object", key)
}

func getRequestString(obj ast.Object, key string) (string, error) {
	s := obj.Get(ast.StringTerm(key))
	if s == nil {
		return "", builtins.NewOperandErr(1, "'%s' missing", key)
	}

	if s, ok := s.Value.(ast.String); ok {
		return string(s), nil
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

func getRequestIntWithDefault(obj ast.Object, key string, def int) (int, error) {
	v := obj.Get(ast.StringTerm(key))
	if v == nil {
		return def, nil
	}

	switch n := v.Value.(type) {
	case ast.Number:
		i, ok := n.Int()
		if !ok {
			return 0, fmt.Errorf("invalid number value %v, must be int", v)
		}
		return i, nil

	case ast.String:
		var err error
		i, err := strconv.ParseInt(string(n), 10, 32)
		if err == nil {
			return 0, fmt.Errorf("invalid string value %v, must be integer", v)
		}

		return int(i), nil

	default:
		return 0, builtins.NewOperandErr(1, "'int32' must be one of {string, number} but got %s", ast.TypeName(n))
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

func checkCaches(bctx topdown.BuiltinContext, req ast.Object, interQueryCacheEnabled bool, cacheKey interface{}, hitsKey string) (ast.Value, bool, error) {
	if interQueryCacheEnabled {
		if resp, ok, err := checkInterQueryCache(bctx, req, hitsKey); ok {
			return resp, true, err
		}
	}

	return checkIntraQueryCache(bctx, req, cacheKey)
}

func checkInterQueryCache(bctx topdown.BuiltinContext, req ast.Object, hitsKey string) (ast.Value, bool, error) {
	cache := bctx.InterQueryBuiltinCache

	// TODO: Cache keys will not overlap with the http.send cache
	// keys because sql.send and http.send have each unique
	// required keys in their request objects. This is definitely
	// not an ideal arrangement to guarantee the isolation between
	// the two builtins.

	key := req
	serializedResp, found := cache.Get(key)
	if !found {
		return nil, false, nil
	}

	resp, err := serializedResp.(*interQueryCacheEntry).Unmarshal()
	if err != nil {
		return nil, true, err
	}

	if getCurrentTime(bctx).Before(resp.ExpiresAt) {
		bctx.Metrics.Counter(hitsKey).Incr()
		resp, err := resp.FormatToAST()
		return resp, true, err
	}

	// No valid entry found.

	return nil, false, nil
}

func checkIntraQueryCache(bctx topdown.BuiltinContext, req ast.Object, cacheKey interface{}) (ast.Value, bool, error) {
	if v := getIntraQueryCache(bctx, cacheKey).Get(req); v != nil {
		// It's safe not to clone the response as the VM will
		// convert the AST types into its internal
		// representation anyway.
		return v.Response, true, v.Error
	}

	return nil, false, nil
}

func getIntraQueryCache(bctx topdown.BuiltinContext, cacheKey interface{}) *intraQueryCache {
	raw, ok := bctx.Cache.Get(cacheKey)
	if !ok {
		c := newIntraQueryCache()
		bctx.Cache.Put(cacheKey, c)
		return c
	}

	return raw.(*intraQueryCache)
}

func insertCaches(bctx topdown.BuiltinContext, req ast.Object, resp ast.Object, queryErr error, interQueryCacheEnabled bool, ttl time.Duration, cacheKey interface{}) error {
	if queryErr == nil && interQueryCacheEnabled {
		// Only cache successful queries for across queries;
		// currently we can't separate between transient
		// errors (e.g., network issues) and persistent errors
		// (e.g., query syntax). Hence, it's impossible to
		// know when queries actually warrant for retries and
		// should not be cached.
		if err := insertInterQueryCache(bctx, req, resp, ttl); err != nil {
			return err
		}
	}

	// Within a query we expect deterministic results, hence cache
	// errors too.

	insertIntraQueryCache(bctx, req, resp, queryErr, cacheKey)
	return nil
}

func insertInterQueryCache(bctx topdown.BuiltinContext, req ast.Object, resp ast.Object, ttl time.Duration) error {
	entry, err := newInterQueryCacheEntry(bctx, resp, ttl)
	if err != nil {
		return err
	}

	bctx.InterQueryBuiltinCache.Insert(req, entry)
	return nil
}

func insertIntraQueryCache(bctx topdown.BuiltinContext, req ast.Object, resp ast.Object, queryErr error, cacheKey interface{}) {
	if queryErr == nil {
		getIntraQueryCache(bctx, cacheKey).PutResponse(req, resp)
	} else {
		getIntraQueryCache(bctx, cacheKey).PutError(req, queryErr)
	}
}

func newInterQueryCacheEntry(bctx topdown.BuiltinContext, resp ast.Object, ttl time.Duration) (*interQueryCacheEntry, error) {
	data, err := newInterQueryCacheData(bctx, resp, ttl)
	if err != nil {
		return nil, err
	}

	return data.Marshal()
}

func (e interQueryCacheEntry) SizeInBytes() int64 {
	return int64(len(e.Data))
}

func (e interQueryCacheEntry) Unmarshal() (*interQueryCacheData, error) {
	var data interQueryCacheData
	err := util.UnmarshalJSON(e.Data, &data)
	return &data, err
}

func (e interQueryCacheEntry) Clone() (cache.InterQueryCacheValue, error) {
	return e, nil
}

func newInterQueryCacheData(bctx topdown.BuiltinContext, resp ast.Object, ttl time.Duration) (*interQueryCacheData, error) {
	r, err := ast.JSONWithOpt(resp, ast.JSONOpt{})
	if err != nil {
		return nil, err
	}

	return &interQueryCacheData{
		Response:  r,
		ExpiresAt: getCurrentTime(bctx).Add(ttl),
	}, nil
}

func (c *interQueryCacheData) FormatToAST() (ast.Value, error) {
	return ast.InterfaceToValue(c.Response)
}

func (c *interQueryCacheData) Marshal() (*interQueryCacheEntry, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	return &interQueryCacheEntry{Data: b}, nil
}

func (*interQueryCacheData) SizeInBytes() int64 {
	return 0
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

func (cache *intraQueryCache) PutResponse(key ast.Value, response ast.Object) {
	cache.entries.Put(key, intraQueryCacheEntry{Response: response})
}

func (cache *intraQueryCache) PutError(key ast.Value, err error) {
	cache.entries.Put(key, intraQueryCacheEntry{Error: err})
}

func Init() {
	BuiltinMap = map[string]*ast.Builtin{}
	for _, b := range DefaultBuiltins {
		RegisterBuiltin(b)     // Only used for generating Enterprise OPA-specific capabilities.
		ast.RegisterBuiltin(b) // Normal builtin registration with OPA.
	}
	for name, fn := range builtinFunctions {
		topdown.RegisterBuiltinFunc(name, fn)
	}
	updateCaps()
}

func enterpriseOPAExtensions(f *ast.Capabilities) {
	features := strings.Split("bjson_bundle,grpc_service,kafka_data_plugin,git_data_plugin,ldap_data_plugin,s3_data_plugin,okta_data_plugin,http_data_plugin,lia_plugin", ",")
	f.Features = append(f.Features, features...)
	f.Builtins = append(f.Builtins, Builtins...)
}

func docs(u string, extra ...string) []string {
	return append(extra, fmt.Sprintf("url=%s", u))
}
