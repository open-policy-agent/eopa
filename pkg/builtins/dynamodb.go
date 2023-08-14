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

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
)

const (
	dynamoDBSendName = "dynamodb.send"
	// dynamoDBSendBuiltinCacheKey is the key in the builtin context cache that
	// points to the dynamodb.send() specific intra-query cache resides at.
	dynamoDBSendBuiltinCacheKey            dynamoDBSendKey = "DYNAMODB_SEND_CACHE_KEY"
	dynamoDBInterQueryCacheDurationDefault                 = 60 * time.Second
)

var (
	dynamoDBClients = dynamoDBClientPool{clients: make(map[dynamoDBClientKey]dynamodbiface.DynamoDBAPI)}

	dynamoDBAllowedKeys = ast.NewSet(
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("credentials"), // environment variables used if no credentials provided
		ast.StringTerm("endpoint"),
		ast.StringTerm("get"),
		ast.StringTerm("raise_error"),
		ast.StringTerm("region"),
	)

	dynamoDBRequiredKeys = ast.NewSet(ast.StringTerm("get"), ast.StringTerm("region"))

	// Marked non-deterministic because DynamoDB query results can be non-deterministic.
	dynamoDBSend = &ast.Builtin{
		Name:        dynamoDBSendName,
		Description: "Returns query result rows to the given DynamoDB operation.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))),
		),
		Nondeterministic: true,
	}

	dynamoDBSendLatencyMetricKey    = "rego_builtin_dynamodb_send"
	dynamoDBSendInterQueryCacheHits = dynamoDBSendLatencyMetricKey + "_interquery_cache_hits"
)

type (
	dynamoDBClientPool struct {
		mu      sync.Mutex
		clients map[dynamoDBClientKey]dynamodbiface.DynamoDBAPI
	}

	dynamoDBClientKey struct {
		endpoint     string
		region       string
		accessKey    string
		secretKey    string
		sessionToken string
	}

	dynamoDBSendKey string
)

func builtinDynamoDBSend(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, span := otel.Tracer(dynamoDBSendName).Start(bctx.Context, "execute")
	defer span.End()

	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(dynamoDBAllowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameter(s): %v", invalidKeys)
	}

	missingKeys := dynamoDBRequiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameter(s): %v", missingKeys)
	}

	get, err := getRequestObject(obj, "get")
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	region, err := getRequestStringWithDefault(obj, "region", "")
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	endpoint, err := getRequestStringWithDefault(obj, "endpoint", "")
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	credentials, err := getRequestObjectWithDefault(obj, "credentials", ast.NewObject())
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	accessKey, err := getRequestStringWithDefault(credentials, "access_key", "")
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	secretKey, err := getRequestStringWithDefault(credentials, "secret_key", "")
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	sessionToken, err := getRequestStringWithDefault(credentials, "session_token", "")
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	raiseError, err := getRequestBoolWithDefault(obj, "raise_error", true)
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	interQueryCacheEnabled, err := getRequestBoolWithDefault(obj, "cache", false)
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	ttl, err := getRequestTimeoutWithDefault(obj, "cache_duration", interQueryCacheDurationDefault)
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	// TODO: Improve error handling to allow separation between
	// types of errors (invalid queries, connectivity errors,
	// etc.)

	table, err := getRequestString(get, "table")
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	consistentRead, err := getRequestBoolWithDefault(get, "consistent_read", false)
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	key, err := getRequestObject(get, "key")
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	// TODO: Projection expression and expression attribute names.

	bctx.Metrics.Timer(dynamoDBSendLatencyMetricKey).Start()

	if responseObj, ok, err := checkCaches(bctx, get, interQueryCacheEnabled && !consistentRead, dynamoDBSendBuiltinCacheKey, dynamoDBSendInterQueryCacheHits); ok {
		if err != nil {
			return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
		}

		return iter(ast.NewTerm(responseObj))
	}

	result, queryErr := func() (map[string]interface{}, error) {
		client, err := dynamoDBClients.Get(bctx.Context, region, endpoint, accessKey, secretKey, sessionToken)
		if err != nil {
			return nil, err
		}

		key, err := ast.JSON(key)
		if err != nil {
			return nil, err
		}

		m := make(map[string]*dynamodb.AttributeValue)
		for k, v := range key.(map[string]interface{}) {
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

		request := dynamodb.GetItemInput{
			ConsistentRead: &consistentRead,
			Key:            m,
			TableName:      &table,
		}

		response, err := client.GetItemWithContext(bctx.Context, &request)
		if err != nil {
			return nil, err
		}

		row := make(map[string]interface{})
		err = dynamodbattribute.UnmarshalMap(response.Item, &row)
		return row, err
	}()

	m := map[string]interface{}{}
	if len(result) > 0 {
		m["row"] = result
	}

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
			return handleBuiltinErr(dynamoDBSendName, bctx.Location, queryErr)
		}
	}

	responseObj, err := ast.InterfaceToValue(m)
	if err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	if err := insertCaches(bctx, get, responseObj.(ast.Object), queryErr, interQueryCacheEnabled && !consistentRead, ttl, dynamoDBSendBuiltinCacheKey); err != nil {
		return handleBuiltinErr(dynamoDBSendName, bctx.Location, err)
	}

	bctx.Metrics.Timer(dynamoDBSendLatencyMetricKey).Stop()

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

func init() {
	topdown.RegisterBuiltinFunc(dynamoDBSendName, builtinDynamoDBSend)
}
