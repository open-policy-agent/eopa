package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"

	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func TestDynamoDBSend(t *testing.T) {
	ddb, endpoint := startDynamoDB(t)
	defer ddb.Terminate(context.Background())

	now := time.Now()

	tests := []struct {
		note                string
		source              string
		result              string
		error               string
		doNotResetCache     bool
		time                time.Time
		interQueryCacheHits int
	}{
		{
			note:                "missing parameter(s)",
			source:              `p = resp { dynamodb.send({}, resp)}`,
			result:              "",
			error:               `eval_type_error: dynamodb.send: operand 1 missing required request parameter(s): {"region"}`,
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "non-existing row query",
			source:              fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "nonexisting"}, "s": {"N": "1"}}}})`, endpoint),
			result:              `{{"result": {"p": {}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "a single row query",
			source:              fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "a multi row query",
			source:              fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "query": {"table": "foo", "key_condition_expression": "#p = :value", "expression_attribute_values": {":value": {"S": "x"}}, "expression_attribute_names": {"#p": "p"}}})`, endpoint),
			result:              `{{"result": {"p": {"rows": [{"number": 1234, "p": "x", "s": 1, "string": "value"},{"number": 4321, "p": "x", "s": 2, "string": "value2"}]}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note: "intra-query query cache",
			source: fmt.Sprintf(`p = [ resp1, resp2 ] {
                                dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}}, resp1)
                                dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}}, resp2) # cached
				}`, endpoint, endpoint),
			result:              `{{"result": {"p": [{"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}, {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}]}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache warmup (default duration)",
			source:              fmt.Sprintf(`p := dynamodb.send({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (default duration, valid)",
			source:              fmt.Sprintf(`p := dynamodb.send({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault - 1),
			interQueryCacheHits: 1,
		},
		{
			note:                "inter-query query cache check (default duration, expired)",
			source:              fmt.Sprintf(`p := dynamodb.send({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault),
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache warmup (explicit duration)",
			source:              fmt.Sprintf(`p := dynamodb.send({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "inter-query query cache check (explicit duration, valid)",
			source:              fmt.Sprintf(`p := dynamodb.send({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10*time.Second - 1),
			interQueryCacheHits: 1,
		},
		{
			note:                "inter-query query cache check (explicit duration, expired)",
			source:              fmt.Sprintf(`p := dynamodb.send({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			result:              `{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			error:               "",
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10 * time.Second),
			interQueryCacheHits: 0,
		},
		{
			note:                "error w/o raise",
			source:              fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"S": "1"}}}})`, endpoint),
			result:              "",
			error:               "eval_builtin_error: dynamodb.send: ValidationException: One or more parameter values were invalid: Type mismatch for key",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
		{
			note:                "error with raise",
			source:              fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"S": "1"}}}, "raise_error": false})`, endpoint),
			result:              `{{"result": {"p": {"error": {"code": "ValidationException", "message": "One or more parameter values were invalid: Type mismatch for key"}}}}}`,
			error:               "",
			doNotResetCache:     false,
			time:                now,
			interQueryCacheHits: 0,
		},
	}

	var maxSize int64 = 1024 * 1024
	interQueryCache := cache.NewInterQueryCache(&cache.Config{
		InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
			MaxSizeBytes: &maxSize,
		},
	})

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			if !tc.doNotResetCache {
				interQueryCache = cache.NewInterQueryCache(&cache.Config{
					InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
						MaxSizeBytes: &maxSize,
					},
				})
			}

			executeDynamoDB(t, interQueryCache, "package t\n"+tc.source, "t", tc.result, tc.error, tc.time, tc.interQueryCacheHits)
		})
	}
}

func executeDynamoDB(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, time time.Time, expectedInterQueryCacheHits int) {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(module),
				Parsed: ast.MustParseModule(module),
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(query)
	if err := compiler.Build(context.Background()); err != nil {
		tb.Fatal(err)
	}

	var policy ir.Policy
	if err := json.Unmarshal(compiler.Bundle().PlanModules[0].Raw, &policy); err != nil {
		tb.Fatal(err)
	}

	executable, err := vm.NewCompiler().WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}

	_, ctx := vm.WithStatistics(context.Background())
	metrics := metrics.New()
	v, err := vm.NewVM().WithExecutable(executable).Eval(ctx, query, vm.EvalOpts{
		Metrics:                metrics,
		Time:                   time,
		Cache:                  builtins.Cache{},
		InterQueryBuiltinCache: interQueryCache,
		StrictBuiltinErrors:    true,
	})
	if expectedError != "" {
		if !strings.HasPrefix(err.Error(), expectedError) {
			tb.Fatalf("unexpected error: %v", err)
		}

		return
	}
	if err != nil {
		tb.Fatal(err)
	}

	if t := ast.MustParseTerm(expectedResult); v.Compare(t.Value) != 0 {
		tb.Fatalf("got %v wanted %v\n", v, expectedResult)
	}

	if hits := metrics.Counter(dynamoDBSendInterQueryCacheHits).Value().(uint64); hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}
}

func startDynamoDB(t *testing.T) (testcontainers.Container, string) {
	t.Helper()

	ctx := context.Background()
	ddb, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "amazon/dynamodb-local:latest",
			Cmd:          []string{"-jar", "DynamoDBLocal.jar", "-inMemory", "-sharedDb"},
			ExposedPorts: []string{"8000/tcp"},
			WaitingFor:   wait.NewHostPortStrategy("8000"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ip, err := ddb.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}

	port, err := ddb.MappedPort(ctx, "8000")
	if err != nil {
		t.Fatal(err)
	}

	endpoint := fmt.Sprintf("http://%s:%s", ip, port)

	// Create the test table(s). The first operation may require
	// retrying as the container is occasionally incomplete even
	// with the wait-port-strategy above.

	svc := dynamodb.New(session.Must(session.NewSession((&aws.Config{
		Endpoint: aws.String(endpoint),
		Region:   aws.String("us-west-2"),
	}).WithCredentials(credentials.NewStaticCredentials("dummy", "dummy", "")))))

	for {
		if _, err := svc.CreateTable(&dynamodb.CreateTableInput{
			BillingMode: aws.String("PAY_PER_REQUEST"),
			TableName:   aws.String("foo"),
			KeySchema: []*dynamodb.KeySchemaElement{
				{
					AttributeName: aws.String("p"),
					KeyType:       aws.String("HASH"),
				},
				{
					AttributeName: aws.String("s"),
					KeyType:       aws.String("RANGE"),
				},
			},
			AttributeDefinitions: []*dynamodb.AttributeDefinition{
				{
					AttributeName: aws.String("p"),
					AttributeType: aws.String("S"),
				},
				{
					AttributeName: aws.String("s"),
					AttributeType: aws.String("N"),
				},
			},
		}); err != nil {
			t.Logf("CreateTable failed, retrying: %v", err.Error())
			time.Sleep(100 * time.Millisecond)
			continue
		}

		break
	}

	if err := svc.WaitUntilTableExists(&dynamodb.DescribeTableInput{
		TableName: aws.String("foo"),
	}); err != nil {
		t.Fatal(err)
	}

	putItems := []dynamodb.PutItemInput{
		{
			TableName: aws.String("foo"),
			Item: map[string]*dynamodb.AttributeValue{
				"p":      {S: aws.String("x")},
				"s":      {N: aws.String("1")},
				"string": {S: aws.String("value")},
				"number": {N: aws.String("1234")},
			},
		},
		{
			TableName: aws.String("foo"),
			Item: map[string]*dynamodb.AttributeValue{
				"p":      {S: aws.String("x")},
				"s":      {N: aws.String("2")},
				"string": {S: aws.String("value2")},
				"number": {N: aws.String("4321")},
			},
		},
	}

	for _, input := range putItems {
		if _, err := svc.PutItem(&input); err != nil {
			t.Fatal(err)
		}
	}

	return ddb, endpoint
}
