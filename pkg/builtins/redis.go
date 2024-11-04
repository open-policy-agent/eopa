package builtins

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
	"go.opentelemetry.io/otel"

	"github.com/redis/go-redis/v9"
)

const (
	redisQueryName            = "redis.query"
	redisQueryKey             = "REDIS_QUERY_CACHE_KEY"
	redisQueryBuiltinCacheKey = "REDIS_QUERY_CACHE_KEY"

	redisQueryLatencyMetricKey       = "rego_builtin_redis_query"
	redisQueryInterQueryCacheHitsKey = redisQueryLatencyMetricKey + "_interquery_cache_hits"
)

// getRedisResult internally uses a type switch on c to extract a genericized
// version of the result.
func getRedisResult(c redis.Cmder) (any, error) {
	switch m := c.(type) {
	case *redis.StringCmd:
		return m.Result()
	case *redis.StringSliceCmd:
		return m.Result()
	case *redis.IntCmd:
		return m.Result()
	case *redis.BoolCmd:
		return m.Result()
	case *redis.BoolSliceCmd:
		return m.Result()
	case *redis.MapStringStringCmd:
		return m.Result()
	case *redis.SliceCmd:
		return m.Result()
	default:
		panic("if you are reading this message, you forgot to update the type switch in getRedisResult() after adding a novel command type to redisAllowedCommands (this should be unreachable in production builds of EOPA)")
	}
}

// wrapCmder is necessary to construct the redisAllowedCommands, since the Go
// compiler is not smart enough to realize on its own that the return types of
// the individual functions return redis.Cmder compatible pointers. The issue
// that this function solves is that the concrete types of the `New*Cmd()`
// functions won't be automatically coerced by the Go compiler into
// `func(context.Context, ...any) redis.Cmder`, which would prevent them
// from going into that map. We specifically need these functions for later
// because we don't know what arguments will be passed as arguments to them
// until a caller of the Rego builtin actually tries to instantiate a Redis
// command.
func wrapCmder[T redis.Cmder](f func(context.Context, ...any) T) func(context.Context, ...any) redis.Cmder {
	return func(ctx context.Context, args ...any) redis.Cmder {
		return f(ctx, args...)
	}
}

var (

	// Only commands present in this map are permitted to be used, to
	// ensure that the databases is only accessed in a read-only way. The
	// values should be the proper constructor function to create the Redis
	// command; this is necessary because the type of the command struct is
	// used within the Redis client to decide how to parse the server
	// response.
	redisAllowedCommands = map[string]func(context.Context, ...any) redis.Cmder{
		// strings
		"get":      wrapCmder(redis.NewStringCmd),
		"getrange": wrapCmder(redis.NewStringCmd),
		"mget":     wrapCmder(redis.NewSliceCmd),
		"strlen":   wrapCmder(redis.NewIntCmd),

		// lists
		"lindex": wrapCmder(redis.NewStringCmd),
		"llen":   wrapCmder(redis.NewIntCmd),
		"lrange": wrapCmder(redis.NewStringSliceCmd),
		"lpos":   wrapCmder(redis.NewIntCmd),

		// hashes
		"hget":       wrapCmder(redis.NewStringCmd),
		"hgetall":    wrapCmder(redis.NewMapStringStringCmd),
		"hexists":    wrapCmder(redis.NewBoolCmd),
		"hkeys":      wrapCmder(redis.NewStringSliceCmd),
		"hlen":       wrapCmder(redis.NewIntCmd),
		"hmget":      wrapCmder(redis.NewSliceCmd),
		"hrandfield": wrapCmder(redis.NewStringSliceCmd),

		// sets
		"scard":       wrapCmder(redis.NewIntCmd),
		"sdiff":       wrapCmder(redis.NewStringSliceCmd),
		"sinter":      wrapCmder(redis.NewStringSliceCmd),
		"sintercard":  wrapCmder(redis.NewIntCmd),
		"sismember":   wrapCmder(redis.NewBoolCmd),
		"smembers":    wrapCmder(redis.NewStringSliceCmd),
		"smismember":  wrapCmder(redis.NewBoolSliceCmd),
		"srandmember": wrapCmder(redis.NewStringCmd),
		"sunion":      wrapCmder(redis.NewStringSliceCmd),
	}

	redisQueryAllowedKeys = ast.NewSet(
		ast.StringTerm("auth"),
		ast.StringTerm("addr"),
		ast.StringTerm("db"),
		ast.StringTerm("command"),
		ast.StringTerm("args"),
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("raise_error"),
	)

	redisAuthAllowedKeys = ast.NewSet(
		ast.StringTerm("username"),
		ast.StringTerm("password"),
		ast.StringTerm("protocol"),
	)

	redisQueryRequiredKeys = ast.NewSet(
		ast.StringTerm("addr"),
		ast.StringTerm("command"),
		ast.StringTerm("args"),
	)

	redisQuery = &ast.Builtin{
		Name:        redisQueryName,
		Description: "Returns the result of the given Redis command.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("query object"),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("response object"),
		),
		Nondeterministic: true,
		Categories:       docs("https://docs.styra.com/enterprise-opa/reference/built-in-functions/redis"),
	}
)

func builtinredisQuery(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, span := otel.Tracer(redisQueryName).Start(bctx.Context, "execute")
	defer span.End()

	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return err
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(redisQueryAllowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameter(s): %v", invalidKeys)
	}

	missingKeys := redisQueryRequiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameter(s): %v", missingKeys)
	}

	cacheKey := obj
	auth, err := getRequestObjectWithDefault(obj, "auth", ast.NewObject())
	if err != nil {
		return err
	} else if auth != nil {
		// We already verified that the auth key is present, so auth
		// cannot be nil, and we don't need to handle that case.

		authKeys := ast.NewSet(auth.Keys()...)
		invalidAuthKeys := authKeys.Diff(redisAuthAllowedKeys)
		if invalidAuthKeys.Len() != 0 {
			return builtins.NewOperandErr(pos, "invalid request auth parameter(s): %v", invalidAuthKeys)
		}
	}

	interQueryCacheEnabled, err := getRequestBoolWithDefault(obj, "cache", false)
	if err != nil {
		return err
	}

	ttl, err := getRequestTimeoutWithDefault(obj, "cache_duration", interQueryCacheDurationDefault)
	if err != nil {
		return err
	}

	raiseError, err := getRequestBoolWithDefault(obj, "raise_error", true)
	if err != nil {
		return err
	}

	bctx.Metrics.Timer(redisQueryLatencyMetricKey).Start()
	defer bctx.Metrics.Timer(redisQueryLatencyMetricKey).Stop()

	if responseObj, ok, err := checkCaches(bctx, cacheKey, interQueryCacheEnabled, redisQueryBuiltinCacheKey, redisQueryInterQueryCacheHitsKey); ok {
		if err != nil {
			return err
		}

		return iter(ast.NewTerm(responseObj))
	}

	// cannot be nil, because args is in the required keys list
	argsT := obj.Get(ast.StringTerm("args"))
	argsI, err := ast.ValueToInterface(argsT.Value, nil)
	var args []any
	if err != nil {
		return err
	}
	if _, ok := argsI.([]any); ok {
		args = argsI.([]any)
	} else {
		return builtins.NewOperandErr(pos, "expected args to be a list ([]any), not %T", argsI)
	}

	// convert any json.Number values into float64, otherwise Redis will
	// fail with 'redis: can't marshal json.Number (implement
	// encoding.BinaryMarshaler)'
	for i, v := range args {
		if n, ok := v.(json.Number); ok {
			var err error
			args[i], err = n.Float64()
			if err != nil {
				builtins.NewOperandErr(pos, "args[%d] ('%+v') is a number, but cannot be converted to float64 due to error: %w", i, v, err)
			}
		}
	}

	command, err := getRequestString(obj, "command")
	if err != nil {
		return err
	}
	addr, err := getRequestStringWithDefault(obj, "addr", "")
	if err != nil {
		return err
	}
	db, err := getRequestIntWithDefault(obj, "db", 0)
	if err != nil {
		return err
	}

	username, err := getRequestStringWithDefault(auth, "username", "")
	if err != nil {
		return err
	}
	password, err := getRequestStringWithDefault(auth, "password", "")
	if err != nil {
		return err
	}
	protocol, err := getRequestIntWithDefault(auth, "protocol", 3)
	if err != nil {
		return err
	}

	// We could also create an Options object by using redis.ParseURL(),
	// but that duplicates the functionality of the auth object; I suspect
	// people may reasonably want field-level control over the information
	// in these options for use with vault helpers.
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		Username: username,
		DB:       db,
		Protocol: protocol,
	})

	cmdConstructor, ok := redisAllowedCommands[strings.ToLower(command)]
	if !ok {
		return builtins.NewOperandErr(pos, "redis command '%s' is not supported", command)
	}

	cmdWithArgs := append([]any{command}, args...)
	cmd := cmdConstructor(bctx.Context, cmdWithArgs...)
	queryErr := rdb.Process(bctx.Context, cmd)

	m := map[string]any{}

	if queryErr != nil {
		if raiseError {
			m["error"] = queryErr.Error()
			queryErr = nil
		} else {
			return queryErr
		}
	} else {
		m["results"], err = getRedisResult(cmd)
		if err != nil {

			// NOTE: It's not clear to me that this case can
			// actually occur in practice. I'm not sure what it
			// would mean semantically for executing a command to
			// return a nil error, but retrieving its result to be
			// non-nil. Nevertheless, we should report it if that
			// happens.
			//
			// -- CAD 2024-03-05

			if raiseError {
				m["error"] = err.Error()
			} else {
				return err
			}
		}

	}

	responseObj, err := ast.InterfaceToValue(m)
	if err != nil {
		return err
	}

	if err := insertCaches(bctx, cacheKey, responseObj.(ast.Object), queryErr, interQueryCacheEnabled, ttl, redisQueryInterQueryCacheHitsKey); err != nil {
		return err
	}

	return iter(ast.NewTerm(responseObj))

}

func init() {
	RegisterBuiltinFunc(redisQueryName, builtinredisQuery)
}
