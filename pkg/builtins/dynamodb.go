// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithyendpoints "github.com/aws/smithy-go/endpoints"

	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	opaTypes "github.com/open-policy-agent/opa/v1/types"
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
	dynamoDBClients = dynamoDBClientPool{clients: make(map[dynamoDBClientKey]*dynamodb.Client)}

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
		Decl: opaTypes.NewFunction(
			opaTypes.Args(
				opaTypes.Named("request", opaTypes.NewObject(nil, opaTypes.NewDynamicProperty(opaTypes.S, opaTypes.A))).Description("query object"),
			),
			opaTypes.Named("response", opaTypes.NewObject(nil, opaTypes.NewDynamicProperty(opaTypes.A, opaTypes.A))).Description("result row"),
		),
		Nondeterministic: true,
		Categories:       docs("https://docs.styra.com/enterprise-opa/reference/built-in-functions/dynamodb"),
	}

	dynamoDBQuery = &ast.Builtin{
		Name:        dynamoDBQueryName,
		Description: "Returns DynamoDB query result rows.",
		Decl: opaTypes.NewFunction(
			opaTypes.Args(
				opaTypes.Named("request", opaTypes.NewObject(nil, opaTypes.NewDynamicProperty(opaTypes.S, opaTypes.A))).Description("query object"),
			),
			opaTypes.Named("response", opaTypes.NewObject(nil, opaTypes.NewDynamicProperty(opaTypes.A, opaTypes.A))).Description("result row"),
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
		clients map[dynamoDBClientKey]*dynamodb.Client
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

	m := map[string]any{}
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

		request := &dynamodb.GetItemInput{
			ConsistentRead: aws.Bool(consistentRead),
			Key:            key,
			TableName:      aws.String(table),
		}

		response, err := client.GetItem(bctx.Context, request)
		if err != nil {
			return err
		}

		row := make(map[string]any)
		err = attributevalue.UnmarshalMap(response.Item, &row)

		if len(row) > 0 {
			m["row"] = row
		}

		return err
	}()

	if queryErr != nil {
		var ae smithy.APIError
		if !raiseError {
			// Unpack the driver specific error type to
			// get more details, if possible.
			e := map[string]any{}

			if errors.As(queryErr, &ae) {
				e["code"] = ae.ErrorCode()
				e["message"] = ae.ErrorMessage()
			} else {
				e["message"] = string(queryErr.Error())
			}

			m["error"] = e
			queryErr = nil
		} else {
			if errors.As(queryErr, &ae) {
				return ae
			}
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

	m := map[string]any{}
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

	paramInt := func(i int) *int32 {
		if i <= 0 {
			return nil
		}

		j := int32(i)
		return &j
	}

	paramSelect := func(s string) types.Select {
		if s == "" {
			return ""
		}
		return types.Select(s)
	}

	queryErr = func() error {
		client, err := dynamoDBClients.Get(bctx.Context, region, endpoint, accessKey, secretKey, sessionToken)
		if err != nil {
			return err
		}

		request := &dynamodb.QueryInput{
			ConsistentRead:            aws.Bool(consistentRead),
			ExclusiveStartKey:         exclusiveStartKey,
			ExpressionAttributeNames:  expressionAttributeNames,
			ExpressionAttributeValues: expressionAttributeValues,
			IndexName:                 paramString(indexName),
			KeyConditionExpression:    paramString(keyConditionExpression),
			Limit:                     paramInt(limit),
			ProjectionExpression:      paramString(projectionExpression),
			ScanIndexForward:          aws.Bool(scanIndexForward),
			Select:                    paramSelect(selekt),
			TableName:                 aws.String(table),
		}

		// Use v2 paginator
		paginator := dynamodb.NewQueryPaginator(client, request)
		var rows []map[string]any

		for paginator.HasMorePages() {
			output, err := paginator.NextPage(bctx.Context)
			if err != nil {
				return err
			}
			for _, item := range output.Items {
				row := make(map[string]any)
				if err := attributevalue.UnmarshalMap(item, &row); err != nil {
					return err
				}
				rows = append(rows, row)
			}
		}

		if len(rows) > 0 {
			m["rows"] = rows
		}

		return nil
	}()

	if queryErr != nil {
		var ae smithy.APIError
		if !raiseError {
			// Unpack the driver specific error type to
			// get more details, if possible.
			e := map[string]any{}

			if errors.As(queryErr, &ae) {
				e["code"] = ae.ErrorCode()
				e["message"] = ae.ErrorMessage()
			} else {
				e["message"] = string(queryErr.Error())
			}

			m["error"] = e
			queryErr = nil
		} else {
			if errors.As(queryErr, &ae) {
				return ae
			}
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

func (p *dynamoDBClientPool) Get(ctx context.Context, region string, endpoint string, accessKey string, secretKey string, sessionToken string) (*dynamodb.Client, error) {
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
	client, err := p.open(ctx, region, endpoint, accessKey, secretKey, sessionToken)
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

func (p *dynamoDBClientPool) open(ctx context.Context, region string, endpoint string, accessKey string, secretKey string, sessionToken string) (*dynamodb.Client, error) {
	// Start with default config
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithHTTPClient(defaultClient()),
	)
	if err != nil {
		return nil, err
	}

	// Note(philip): Originally, we checked credentials in this chain order:
	// - Statically provided credentials
	// - Environment variables
	// - ECS/EC2 role providers
	// However, after migrating to the AWS SDK v2, we use the default credential
	// chain order because we're forced to, and work around that.
	// The new order:
	// - Statically provided credentials
	// - Default chain
	//   - Environment variables
	//   - Shared Configuration and Shared Credentials files
	// - ECS/EC2 role providers
	var providers []aws.CredentialsProvider

	// Handle credentials
	if accessKey != "" && secretKey != "" {
		// Static credentials provided
		providers = append(providers, credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken))
	}
	// Else: Use default credential chain which includes environment variables and IAM roles
	// This is already handled by LoadDefaultConfigs

	// Append default credentials provider.
	providers = append(providers, cfg.Credentials)

	// Handle web identity token for service accounts
	if tokenFile := os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE"); tokenFile != "" {
		if roleARN := os.Getenv("AWS_ROLE_ARN"); roleARN != "" {
			roleSessionName := os.Getenv("AWS_ROLE_SESSION_NAME")
			if roleSessionName == "" {
				roleSessionName = "eopa-session" // TODO(philip): Is this really needed?
			}

			stsClient := sts.NewFromConfig(cfg)
			providers = append(providers, stscreds.NewWebIdentityRoleProvider(
				stsClient,
				roleARN,
				stscreds.IdentityTokenFile(tokenFile),
				func(o *stscreds.WebIdentityRoleOptions) {
					o.RoleSessionName = roleSessionName
				},
			))
		}
	}

	// Update config's credentials provider with our homemade chain
	cfg.Credentials = newChainProvider(providers...)

	// Create DynamoDB client with optional custom endpoint
	var opts []func(*dynamodb.Options)
	if endpoint != "" {
		opts = append(opts, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
		opts = append(opts, func(o *dynamodb.Options) {
			o.EndpointResolverV2 = newCustomEndpointResolver(endpoint)
		})
	}

	return dynamodb.NewFromConfig(cfg, opts...), nil
}

// In AWS SDK v2, there's no equivalent to the original credentials.NewChainCredentials() func.
// Ref: https://github.com/aws/aws-sdk-go-v2/issues/1433#issuecomment-939537514
func newChainProvider(providers ...aws.CredentialsProvider) aws.CredentialsProvider {
	return aws.NewCredentialsCache(
		aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			var errs []error

			for _, p := range providers {
				if creds, err := p.Retrieve(ctx); err == nil {
					return creds, nil
				} else {
					errs = append(errs, err)
				}
			}

			return aws.Credentials{}, fmt.Errorf("no valid providers in chain: %s", errs)
		}),
	)
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

type customEndpointResolver struct {
	endpoint string // Endpoint value provided from plugin config logic.
}

func newCustomEndpointResolver(endpoint string) *customEndpointResolver {
	return &customEndpointResolver{
		endpoint: endpoint,
	}
}

// Note(philip): This resolver implementation is the workaround needed with the
// AWS SDK v2 to set the endpoint directly like how we were doing in AWS SDK v1.
// If we ever decide to not compute the exact endpoint in our plugin config
// logic, then this implementation might need to change.
func (r *customEndpointResolver) ResolveEndpoint(ctx context.Context, params dynamodb.EndpointParameters) (
	smithyendpoints.Endpoint, error,
) {
	if r.endpoint != "" {
		params.Endpoint = aws.String(r.endpoint)
	}

	return dynamodb.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, params)
}

func getRequestAttributeValuesWithDefault(obj ast.Object, key string, def map[string]types.AttributeValue) (map[string]types.AttributeValue, error) {
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

	o, ok := j.(map[string]any)
	if !ok {
		return nil, builtins.NewOperandErr(1, "'%s' must be object", key)
	}

	m := make(map[string]types.AttributeValue)
	for k, v := range o {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}

		if a, err := attributevalue.UnmarshalJSON(data); err != nil {
			return nil, err
		} else {
			m[k] = a
		}
	}

	return m, nil
}

func getRequestAttributeNamesWithDefault(obj ast.Object, key string, def map[string]string) (map[string]string, error) {
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

	o, ok := j.(map[string]any)
	if !ok {
		return nil, builtins.NewOperandErr(1, "'%s' must be object of strings", key)
	}

	m := make(map[string]string)
	for k, v := range o {
		s, ok := v.(string)
		if !ok {
			return nil, builtins.NewOperandErr(1, "'%s' must be object of strings", key)
		}

		m[k] = s
	}

	return m, nil
}

func init() {
	RegisterBuiltinFunc(dynamoDBGetName, builtinDynamoDBGet)
	RegisterBuiltinFunc(dynamoDBQueryName, builtinDynamoDBQuery)
}
