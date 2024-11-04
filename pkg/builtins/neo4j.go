package builtins

import (
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
	"go.opentelemetry.io/otel"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	neo4jQueryName            = "neo4j.query"
	neo4jQueryKey             = "NEO4J_QUERY_CACHE_KEY"
	neo4jQueryBuiltinCacheKey = "NEO4J_QUERY_CACHE_KEY"

	neo4jQueryLatencyMetricKey       = "rego_builtin_neo4j_query"
	neo4jQueryInterQueryCacheHitsKey = neo4jQueryLatencyMetricKey + "_interquery_cache_hits"
)

var (
	neo4jQueryAllowedKeys = ast.NewSet(
		ast.StringTerm("auth"),
		ast.StringTerm("uri"),
		ast.StringTerm("query"),
		ast.StringTerm("parameters"),
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("raise_error"),
	)

	// see https://github.com/neo4j/neo4j-go-driver/blob/7b4cb82b2b5225307b953946c96f9f876a4643e6/neo4j/auth_tokens.go#L27
	//
	// also see neo4jTokenFromObjet()
	neo4jAuthAllowedKeys = ast.NewSet(
		ast.StringTerm("scheme"),
		ast.StringTerm("principal"),
		ast.StringTerm("credentials"),
		ast.StringTerm("realm"),
	)

	neo4jQueryRequiredKeys = ast.NewSet(
		ast.StringTerm("auth"),
		ast.StringTerm("uri"),
		ast.StringTerm("query"),
	)

	neo4jQuery = &ast.Builtin{
		Name:        neo4jQueryName,
		Description: "Returns results for the given neo4j query.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("query object"),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("response object"),
		),
		Nondeterministic: true,
	}
)

func neo4jTokenFromObject(auth ast.Object) (neo4j.AuthToken, error) {

	// I based this heavily on the internal implementation of the neo4j
	// driver, which can be seen here: https://github.com/neo4j/neo4j-go-driver/blob/v5.14.0/neo4j/auth_tokens.g
	//
	// They don't seem to directly expose ser/des machinery, but the
	// obvious mapping of their code to JSON and back seems reasonable
	// enough. Breaking it out as seen here lets us do validation on the
	// keys in the auth object.
	//
	// It might be worth considering using neo4j.CustomAuth() instead, but
	// we'd still have to do the ser/de logic either way I think, it would
	// just be slightly different. I don't see a clear reason to prefer one
	// approach over the other.
	//
	// -- CAD 2023-11-07

	scheme, err := getRequestString(auth, "scheme")
	if err != nil {
		return neo4j.NoAuth(), err
	}

	switch scheme {
	case "basic":
		password, err := getRequestString(auth, "credentials")
		if err != nil {
			return neo4j.NoAuth(), nil
		}

		username, err := getRequestString(auth, "principal")
		if err != nil {
			return neo4j.NoAuth(), nil
		}

		realm, err := getRequestStringWithDefault(auth, "realm", "")
		if err != nil {
			return neo4j.NoAuth(), nil
		}

		return neo4j.BasicAuth(username, password, realm), nil

	case "kerberos":
		credentials, err := getRequestString(auth, "credentials")
		if err != nil {
			return neo4j.NoAuth(), nil
		}

		return neo4j.KerberosAuth(credentials), nil

	case "bearer":
		credentials, err := getRequestString(auth, "credentials")
		if err != nil {
			return neo4j.NoAuth(), nil
		}

		return neo4j.BearerAuth(credentials), nil

	case "none":
		return neo4j.NoAuth(), nil

	default:
		return neo4j.NoAuth(), fmt.Errorf("unknown neo4j authentication scheme '%s'", scheme)
	}
}

func builtinNeo4jQuery(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, span := otel.Tracer(neo4jQueryName).Start(bctx.Context, "execute")
	defer span.End()

	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return err
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(neo4jQueryAllowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameter(s): %v", invalidKeys)
	}

	missingKeys := neo4jQueryRequiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameter(s): %v", missingKeys)
	}

	cacheKey := obj
	auth, err := getRequestObjectWithDefault(obj, "auth", nil)
	if err != nil {
		return err
	} else if auth != nil {
		// We already verified that the auth key is present, so auth
		// cannot be nil, and we don't need to handle that case.

		authKeys := ast.NewSet(auth.Keys()...)
		invalidAuthKeys := authKeys.Diff(neo4jAuthAllowedKeys)
		if invalidAuthKeys.Len() != 0 {
			return builtins.NewOperandErr(pos, "invalid request auth parameter(s): %v", invalidAuthKeys)
		}
	}

	uri, err := getRequestString(obj, "uri")
	if err != nil {
		return err
	}

	query, err := getRequestString(obj, "query")
	if err != nil {
		return err
	}

	parameters := map[string]any{}
	parametersObj, err := getRequestObjectWithDefault(obj, "parameters", nil)
	if err != nil {
		return err
	}
	// NOTE: we roundtrip through JSON in order to convert any json.Number
	// instances into normal numeric types. Neo4j silently fails to use
	// json.Number in templates.
	//
	// Discussion: https://styra.slack.com/archives/C03TW1U15R8/p1699473023683889
	//
	// -- CAD 2023-11-08
	if parametersObj != nil {
		var parametersRoundTrip any
		err = json.Unmarshal([]byte(parametersObj.String()), &parametersRoundTrip)
		if err != nil {
			return err
		}

		ok := false
		parameters, ok = parametersRoundTrip.(map[string]any)
		if !ok {
			return fmt.Errorf("expected neo4j parameters to have type map[string]any, not %T", parametersRoundTrip)
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

	bctx.Metrics.Timer(neo4jQueryLatencyMetricKey).Start()
	defer bctx.Metrics.Timer(neo4jQueryLatencyMetricKey).Stop()

	if responseObj, ok, err := checkCaches(bctx, cacheKey, interQueryCacheEnabled, neo4jQueryBuiltinCacheKey, neo4jQueryInterQueryCacheHitsKey); ok {
		if err != nil {
			return err
		}

		return iter(ast.NewTerm(responseObj))
	}

	// NOTE: the mongo driver caches it's drivers/clients. It may be worth
	// replicating the same style here, I'm not sure how expensive creating
	// a neo4j driver/session is. For now, I'm avoiding the extra
	// complexity.
	//
	// -- CAD 2023-11-07
	creds, err := neo4jTokenFromObject(auth)
	if err != nil {
		return err
	}
	driver, err := neo4j.NewDriverWithContext(uri, creds)
	if err != nil {
		return err
	}
	defer driver.Close(bctx.Context)
	conf := neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	}
	sess := driver.NewSession(bctx.Context, conf)
	defer sess.Close(bctx.Context)
	var out []map[string]any
	_, queryErr := sess.ExecuteRead(bctx.Context, func(tx neo4j.ManagedTransaction) (any, error) {

		result, err := tx.Run(bctx.Context, query, parameters)
		if err != nil {
			return nil, err
		}

		recs, err := result.Collect(bctx.Context)
		if err != nil {
			return nil, err
		}

		out = make([]map[string]any, 0, len(recs))

		for _, rec := range recs {
			out = append(out, rec.AsMap())
		}

		return nil, nil
	})

	m := map[string]any{}

	if queryErr != nil {
		if !raiseError {
			m["error"] = queryErr.Error()
			queryErr = nil
		} else {
			return queryErr
		}
	} else {
		m["results"] = out
	}

	responseObj, err := ast.InterfaceToValue(m)
	if err != nil {
		return err
	}

	if err := insertCaches(bctx, cacheKey, responseObj.(ast.Object), queryErr, interQueryCacheEnabled, ttl, neo4jQueryInterQueryCacheHitsKey); err != nil {
		return err
	}

	return iter(ast.NewTerm(responseObj))

}

func init() {
	RegisterBuiltinFunc(neo4jQueryName, builtinNeo4jQuery)
}
