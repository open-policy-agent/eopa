package builtins

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/aws/aws-sdk-go/service/sts"
	"go.opentelemetry.io/otel"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/types"
)

const (
	dynamoDBGetName   = "dynamodb.get"
	dynamoDBQueryName = "dynamodb.query"
	// dynamoDBGetBuiltinCacheKey is the key in the builtin context cache that
	// points to the dynamodb.get() specific intra-query cache resides at.
	dynamoDBGetBuiltinCacheKey dynamoDBGetKey = "DYNAMODB_GET_CACHE_KEY"
	// dynamoDBQueryBuiltinCacheKey is the key in the builtin context cache that
	// points to the dynamodb.query() specific intra-query cache resides at.
	dynamoDBQueryBuiltinCacheKey           dynamoDBQueryKey = "DYNAMODB_QUERY_CACHE_KEY"
	dynamoDBInterQueryCacheDurationDefault                  = 60 * time.Second
)

var (
	dynamoDBClients = dynamoDBClientPool{clients: make(map[dynamoDBClientKey]dynamodbiface.DynamoDBAPI)}

	dynamoDBGetAllowedKeys = ast.NewSet(
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("consistent_read"),
		ast.StringTerm("credentials"), // environment variables used if no credentials provided
		ast.StringTerm("endpoint"),
		ast.StringTerm("key"),
		ast.StringTerm("raise_error"),
		ast.StringTerm("region"),
		ast.StringTerm("table"),
	)

	dynamoDBGetRequiredKeys = ast.NewSet(ast.StringTerm("key"), ast.StringTerm("region"), ast.StringTerm("table"))

	dynamoDBQueryAllowedKeys = ast.NewSet(
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("consistent_read"),
		ast.StringTerm("credentials"), // environment variables used if no credentials provided
		ast.StringTerm("endpoint"),
		ast.StringTerm("exclusive_start_key"),
		ast.StringTerm("expression_attribute_names"),
		ast.StringTerm("expression_attribute_values"),
		ast.StringTerm("index_name"),
		ast.StringTerm("key_condition_expression"),
		ast.StringTerm("limit"),
		ast.StringTerm("projection_expression"),
		ast.StringTerm("raise_error"),
		ast.StringTerm("region"),
		ast.StringTerm("scan_index_forward"),
		ast.StringTerm("select"),
		ast.StringTerm("table"),
	)

	dynamoDBQueryRequiredKeys = ast.NewSet(ast.StringTerm("key_condition_expression"), ast.StringTerm("region"), ast.StringTerm("table"))

	// Marked non-deterministic because DynamoDB query results can be non-deterministic.
	dynamoDBGet = &ast.Builtin{
		Name:        dynamoDBGetName,
		Description: "Returns DynamoDB get result row.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("query object"),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("result row"),
		),
		Nondeterministic: true,
		Categories:       docs("https://docs.styra.com/enterprise-opa/reference/built-in-functions/dynamodb"),
	}

	dynamoDBQuery = &ast.Builtin{
		Name:        dynamoDBQueryName,
		Description: "Returns DynamoDB query result rows.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("query object"),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("result row"),
		),
		Nondeterministic: true,
		Categories:       docs("https://docs.styra.com/enterprise-opa/reference/built-in-functions/dynamodb"),
	}

	dynamoDBGetLatencyMetricKey      = "rego_builtin_dynamodb_get"
	dynamoDBGetInterQueryCacheHits   = dynamoDBGetLatencyMetricKey + "_interquery_cache_hits"
	dynamoDBQueryLatencyMetricKey    = "rego_builtin_dynamodb_query"
	dynamoDBQueryInterQueryCacheHits = dynamoDBQueryLatencyMetricKey + "_interquery_cache_hits"
)

type (
	dynamoDBClientPool struct {
		clients map[dynamoDBClientKey]dynamodbiface.DynamoDBAPI
		mu      sync.Mutex
	}

	dynamoDBClientKey struct {
		endpoint     string
		region       string
		accessKey    string
		secretKey    string
		sessionToken string
	}

	dynamoDBGetKey   string
	dynamoDBQueryKey string
)

func builtinDynamoDBGet(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, span := otel.Tracer(dynamoDBGetName).Start(bctx.Context, "execute")
	defer span.End()

	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return err
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(dynamoDBGetAllowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameter(s): %v", invalidKeys)
	}

	missingKeys := dynamoDBGetRequiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameter(s): %v", missingKeys)
	}

	cacheKey := obj

	region, err := getRequestStringWithDefault(obj, "region", "")
	if err != nil {
		return err
	}

	endpoint, err := getRequestStringWithDefault(obj, "endpoint", "")
	if err != nil {
		return err
	}

	credentials, err := getRequestObjectWithDefault(obj, "credentials", ast.NewObject())
	if err != nil {
		return err
	}

	accessKey, err := getRequestStringWithDefault(credentials, "access_key", "")
	if err != nil {
		return err
	}

	secretKey, err := getRequestStringWithDefault(credentials, "secret_key", "")
	if err != nil {
		return err
	}

	sessionToken, err := getRequestStringWithDefault(credentials, "session_token", "")
	if err != nil {
		return err
	}

	raiseError, err := getRequestBoolWithDefault(obj, "raise_error", true)
	if err != nil {
		return err
	}

	interQueryCacheEnabled, err := getRequestBoolWithDefault(obj, "cache", false)
	if err != nil {
		return err
	}

	ttl, err := getRequestTimeoutWithDefault(obj, "cache_duration", interQueryCacheDurationDefault)
	if err != nil {
		return err
	}

	// TODO: Improve error handling to allow separation between
	// types of errors (invalid queries, connectivity errors,
	// etc.)

	table, err := getRequestString(obj, "table")
	if err != nil {
		return err
	}

	consistentRead, err := getRequestBoolWithDefault(obj, "consistent_read", false)
	if err != nil {
		return err
	}

	// TODO: Projection expression and expression attribute names.

	bctx.Metrics.Timer(dynamoDBGetLatencyMetricKey).Start()

	if responseObj, ok, err := checkCaches(bctx, cacheKey, interQueryCacheEnabled && !consistentRead, dynamoDBGetBuiltinCacheKey, dynamoDBGetInterQueryCacheHits); ok {
		if err != nil {
			return err
		}

		return iter(ast.NewTerm(responseObj))
	}

	m := map[string]interface{}{}
	var queryErr error

	key, err := getRequestAttributeValuesWithDefault(obj, "key", nil)
	if err != nil {
		return err
	}

	queryErr = func() error {
		client, err := dynamoDBClients.Get(bctx.Context, region, endpoint, accessKey, secretKey, sessionToken)
		if err != nil {
			return err
		}

		request := dynamodb.GetItemInput{
			ConsistentRead: &consistentRead,
			Key:            key,
			TableName:      &table,
		}

		response, err := client.GetItemWithContext(bctx.Context, &request)
		if err != nil {
			return err
		}

		row := make(map[string]interface{})
		err = dynamodbattribute.UnmarshalMap(response.Item, &row)

		if len(row) > 0 {
			m["row"] = row
		}

		return err
	}()

	if queryErr != nil {
		if !raiseError {
			// Unpack the driver specific error type to
			// get more details, if possible.

			e := map[string]interface{}{}

			switch queryErr := queryErr.(type) {
			case awserr.Error:
				e["code"] = queryErr.Code()
				e["message"] = queryErr.Message()
			default:
				e["message"] = string(queryErr.Error())
			}

			m["error"] = e
			queryErr = nil
		} else {
			return queryErr
		}
	}

	responseObj, err := ast.InterfaceToValue(m)
	if err != nil {
		return err
	}

	if err := insertCaches(bctx, cacheKey, responseObj.(ast.Object), queryErr, interQueryCacheEnabled && !consistentRead, ttl, dynamoDBGetBuiltinCacheKey); err != nil {
		return err
	}

	bctx.Metrics.Timer(dynamoDBGetLatencyMetricKey).Stop()

	return iter(ast.NewTerm(responseObj))
}

func builtinDynamoDBQuery(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, span := otel.Tracer(dynamoDBQueryName).Start(bctx.Context, "execute")
	defer span.End()

	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return err
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(dynamoDBQueryAllowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameter(s): %v", invalidKeys)
	}

	missingKeys := dynamoDBQueryRequiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameter(s): %v", missingKeys)
	}

	cacheKey := obj

	region, err := getRequestStringWithDefault(obj, "region", "")
	if err != nil {
		return err
	}

	endpoint, err := getRequestStringWithDefault(obj, "endpoint", "")
	if err != nil {
		return err
	}

	credentials, err := getRequestObjectWithDefault(obj, "credentials", ast.NewObject())
	if err != nil {
		return err
	}

	accessKey, err := getRequestStringWithDefault(credentials, "access_key", "")
	if err != nil {
		return err
	}

	secretKey, err := getRequestStringWithDefault(credentials, "secret_key", "")
	if err != nil {
		return err
	}

	sessionToken, err := getRequestStringWithDefault(credentials, "session_token", "")
	if err != nil {
		return err
	}

	raiseError, err := getRequestBoolWithDefault(obj, "raise_error", true)
	if err != nil {
		return err
	}

	interQueryCacheEnabled, err := getRequestBoolWithDefault(obj, "cache", false)
	if err != nil {
		return err
	}

	ttl, err := getRequestTimeoutWithDefault(obj, "cache_duration", interQueryCacheDurationDefault)
	if err != nil {
		return err
	}

	// TODO: Improve error handling to allow separation between
	// types of errors (invalid queries, connectivity errors,
	// etc.)

	table, err := getRequestString(obj, "table")
	if err != nil {
		return err
	}

	consistentRead, err := getRequestBoolWithDefault(obj, "consistent_read", false)
	if err != nil {
		return err
	}

	// TODO: Projection expression and expression attribute names.

	bctx.Metrics.Timer(dynamoDBQueryLatencyMetricKey).Start()

	if responseObj, ok, err := checkCaches(bctx, cacheKey, interQueryCacheEnabled && !consistentRead, dynamoDBQueryBuiltinCacheKey, dynamoDBQueryInterQueryCacheHits); ok {
		if err != nil {
			return err
		}

		return iter(ast.NewTerm(responseObj))
	}

	m := map[string]interface{}{}
	var queryErr error

	exclusiveStartKey, err := getRequestAttributeValuesWithDefault(obj, "exclusive_start_key", nil)
	if err != nil {
		return err
	}

	expressionAttributeNames, err := getRequestAttributeNamesWithDefault(obj, "expression_attribute_names", nil)
	if err != nil {
		return err
	}

	expressionAttributeValues, err := getRequestAttributeValuesWithDefault(obj, "expression_attribute_values", nil)
	if err != nil {
		return err
	}

	indexName, err := getRequestStringWithDefault(obj, "index_name", "")
	if err != nil {
		return err
	}

	keyConditionExpression, err := getRequestString(obj, "key_condition_expression")
	if err != nil {
		return err
	}

	limit, err := getRequestIntWithDefault(obj, "limit", 0)
	if err != nil {
		return err
	}

	projectionExpression, err := getRequestStringWithDefault(obj, "projection_expression", "")
	if err != nil {
		return err
	}

	scanIndexForward, err := getRequestBoolWithDefault(obj, "scan_index_forward", true)
	if err != nil {
		return err
	}

	selekt, err := getRequestStringWithDefault(obj, "select", "")
	if err != nil {
		return err
	}

	paramString := func(s string) *string {
		if s == "" {
			return nil
		}

		return &s
	}

	paramInt := func(i int) *int64 {
		if i <= 0 {
			return nil
		}

		j := int64(i)
		return &j
	}

	queryErr = func() error {
		client, err := dynamoDBClients.Get(bctx.Context, region, endpoint, accessKey, secretKey, sessionToken)
		if err != nil {
			return err
		}

		request := dynamodb.QueryInput{
			ConsistentRead:            &consistentRead,
			ExclusiveStartKey:         exclusiveStartKey,
			ExpressionAttributeNames:  expressionAttributeNames,
			ExpressionAttributeValues: expressionAttributeValues,
			IndexName:                 paramString(indexName),
			KeyConditionExpression:    paramString(keyConditionExpression),
			Limit:                     paramInt(limit),
			ProjectionExpression:      paramString(projectionExpression),
			ScanIndexForward:          &scanIndexForward,
			Select:                    paramString(selekt),
			TableName:                 &table,
		}

		var (
			rows []map[string]interface{}
			err2 error
		)

		if err := client.QueryPagesWithContext(bctx.Context, &request, func(output *dynamodb.QueryOutput, _last bool) bool {
			for _, item := range output.Items {
				row := make(map[string]interface{})
				err2 = dynamodbattribute.UnmarshalMap(item, &row)
				if err2 != nil {
					return false
				}

				rows = append(rows, row)
			}

			return true
		}); err != nil {
			return err
		} else if err2 != nil {
			return err2
		}

		if len(rows) > 0 {
			m["rows"] = rows
		}

		return err
	}()

	if queryErr != nil {
		if !raiseError {
			// Unpack the driver specific error type to
			// get more details, if possible.

			e := map[string]interface{}{}

			switch queryErr := queryErr.(type) {
			case awserr.Error:
				e["code"] = queryErr.Code()
				e["message"] = queryErr.Message()
			default:
				e["message"] = string(queryErr.Error())
			}

			m["error"] = e
			queryErr = nil
		} else {
			return queryErr
		}
	}

	responseObj, err := ast.InterfaceToValue(m)
	if err != nil {
		return err
	}

	if err := insertCaches(bctx, cacheKey, responseObj.(ast.Object), queryErr, interQueryCacheEnabled && !consistentRead, ttl, dynamoDBQueryBuiltinCacheKey); err != nil {
		return err
	}

	bctx.Metrics.Timer(dynamoDBQueryLatencyMetricKey).Stop()

	return iter(ast.NewTerm(responseObj))
}

func (p *dynamoDBClientPool) Get(_ context.Context, region string, endpoint string, accessKey string, secretKey string, sessionToken string) (dynamodbiface.DynamoDBAPI, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := dynamoDBClientKey{
		region,
		endpoint,
		accessKey,
		secretKey,
		sessionToken,
	}
	client, ok := p.clients[key]
	if ok {
		return client, nil
	}

	// Do not hold the lock during open, in case it requires I/O.

	p.mu.Unlock()
	client, err := p.open(region, endpoint, accessKey, secretKey, sessionToken)
	p.mu.Lock()

	if err != nil {
		return nil, err
	}

	if existing, ok := p.clients[key]; ok {
		return existing, nil
	}

	p.clients[key] = client
	return client, nil
}

func (p *dynamoDBClientPool) open(region string, endpoint string, accessKey string, secretKey string, sessionToken string) (dynamodbiface.DynamoDBAPI, error) {
	if endpoint == "" {
		re, err := endpoints.AwsPartition().EndpointFor("dynamodb", region)
		if err != nil {
			return nil, err
		}

		endpoint = re.URL
	}

	cfg := aws.NewConfig().WithRegion(region)

	var providers []credentials.Provider

	// Statically provided credentials are always tried and used
	// first.  After which standard AWS environment variables and
	// ECS/EC2 role provider is checked.

	if accessKey != "" && secretKey != "" {
		providers = append(providers, &credentials.StaticProvider{
			Value: credentials.Value{
				AccessKeyID:     accessKey,
				SecretAccessKey: secretKey,
				SessionToken:    sessionToken,
			},
		})
	}

	providers = append(providers, &credentials.EnvProvider{})
	remoteProviders, err := remoteCredProviders(cfg)
	if err != nil {
		return nil, err
	}

	providers = append(providers, remoteProviders...) // Config can't have endpoints set, as it used for STS.

	cfg = cfg.WithEndpoint(endpoint).WithCredentials(credentials.NewChainCredentials(providers))
	cfg = cfg.WithHTTPClient(defaultClient())

	awsSession, err := session.NewSession(cfg)
	if err != nil {
		return nil, err
	}

	return dynamodb.New(awsSession), nil
}

// defaultClient is the HTTP client used with all AWS SDK sessions. It is as http.DefaultClient with one exception: MaxIdleConnsPerHost is increased from default golang value of 2.
func defaultClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			ForceAttemptHTTP2: true,
			MaxIdleConns:      100,
			// This is to avoid constant closing and opening of underlying TCP connections due to high number of parallel SDK calls due to DynamoDB accesses.
			// see https://github.com/golang/go/issues/13801.
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func remoteCredProviders(config *aws.Config) ([]credentials.Provider, error) {
	s, err := session.NewSession(config)
	if err != nil {
		return nil, err
	}

	tokenFile := os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE")
	roleARN := os.Getenv("AWS_ROLE_ARN")

	var providers []credentials.Provider

	if tokenFile != "" && roleARN != "" {
		roleSessionName := os.Getenv("AWS_ROLE_SESSION_NAME")
		providers = append(
			providers,
			stscreds.NewWebIdentityRoleProviderWithOptions(sts.New(s), roleARN, roleSessionName, stscreds.FetchTokenPath(tokenFile)))
	}

	providers = append(providers, defaults.RemoteCredProvider(*config, s.Handlers))

	return providers, nil
}

func getRequestAttributeValuesWithDefault(obj ast.Object, key string, def map[string]*dynamodb.AttributeValue) (map[string]*dynamodb.AttributeValue, error) {
	v, err := getRequestObjectWithDefault(obj, key, nil)
	if err != nil {
		return nil, err
	} else if v == nil {
		return def, nil
	}

	j, err := ast.JSON(v)
	if err != nil {
		return nil, err
	}

	o, ok := j.(map[string]interface{})
	if !ok {
		return nil, builtins.NewOperandErr(1, "'%s' must be object", key)
	}

	m := make(map[string]*dynamodb.AttributeValue)
	for k, v := range o {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}

		var a dynamodb.AttributeValue
		if err := json.Unmarshal(data, &a); err != nil {
			return nil, err
		}

		m[k] = &a
	}

	return m, nil
}

func getRequestAttributeNamesWithDefault(obj ast.Object, key string, def map[string]*string) (map[string]*string, error) {
	v, err := getRequestObjectWithDefault(obj, key, nil)
	if err != nil {
		return nil, err
	} else if v == nil {
		return def, nil
	}

	j, err := ast.JSON(v)
	if err != nil {
		return nil, err
	}

	o, ok := j.(map[string]interface{})
	if !ok {
		return nil, builtins.NewOperandErr(1, "'%s' must be object of strings", key)
	}

	m := make(map[string]*string)
	for k, v := range o {
		s, ok := v.(string)
		if !ok {
			return nil, builtins.NewOperandErr(1, "'%s' must be object of strings", key)
		}

		m[k] = &s
	}

	return m, nil
}

func init() {
	RegisterBuiltinFunc(dynamoDBGetName, builtinDynamoDBGet)
	RegisterBuiltinFunc(dynamoDBQueryName, builtinDynamoDBQuery)
}
