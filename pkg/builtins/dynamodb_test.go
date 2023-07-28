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
			"missing parameter(s)",
			`p = resp { dynamodb.send({}, resp)}`,
			"",
			`eval_type_error: dynamodb.send: operand 1 missing required request parameters(s): {"get", "region"}`,
			false,
			now,
			0,
		},
		{
			"non-existing row query",
			fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "2"}}}})`, endpoint),
			`{{"result": {"p": {}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"a single row query",
			fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			`{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"intra-query query cache",
			fmt.Sprintf(`p = [ resp1, resp2 ] {
                                dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}}, resp1)
                                dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}}, resp2) # cached
				}`, endpoint, endpoint),
			`{{"result": {"p": [{"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}, {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}]}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"inter-query query cache warmup (default duration)",
			fmt.Sprintf(`p := dynamodb.send({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			`{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"inter-query query cache check (default duration, valid)",
			fmt.Sprintf(`p := dynamodb.send({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			`{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			"",
			true, // keep the warmup results
			now.Add(interQueryCacheDurationDefault - 1),
			1,
		},
		{
			"inter-query query cache check (default duration, expired)",
			fmt.Sprintf(`p := dynamodb.send({"cache": true, "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			`{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			"",
			true, // keep the warmup results
			now.Add(interQueryCacheDurationDefault),
			0,
		},
		{
			"inter-query query cache warmup (explicit duration)",
			fmt.Sprintf(`p := dynamodb.send({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			`{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			"",
			false,
			now,
			0,
		},
		{
			"inter-query query cache check (explicit duration, valid)",
			fmt.Sprintf(`p := dynamodb.send({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			`{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			"",
			true, // keep the warmup results
			now.Add(10*time.Second - 1),
			1,
		},
		{
			"inter-query query cache check (explicit duration, expired)",
			fmt.Sprintf(`p := dynamodb.send({"cache": true, "cache_duration": "10s", "credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"N": "1"}}}})`, endpoint),
			`{{"result": {"p": {"row": {"number": 1234, "p": "x", "s": 1, "string": "value"}}}}}`,
			"",
			true, // keep the warmup results
			now.Add(10 * time.Second),
			0,
		},
		{
			"error w/o raise",
			fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"S": "1"}}}})`, endpoint),
			"",
			"eval_builtin_error: dynamodb.send: ValidationException: One or more parameter values were invalid: Type mismatch for key",
			false,
			now,
			0,
		},
		{
			"error with raise",
			fmt.Sprintf(`p := dynamodb.send({"credentials": {"access_key": "key", "secret_key": "key"}, "endpoint": "%s", "region": "us-west-2", "get": {"table": "foo", "key": {"p": {"S": "x"}, "s": {"S": "1"}}}, "raise_error": false})`, endpoint),
			`{{"result": {"p": {"error": {"code": "ValidationException", "message": "One or more parameter values were invalid: Type mismatch for key"}}}}}`,
			"",
			false,
			now,
			0,
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
	}

	for _, input := range putItems {
		if _, err := svc.PutItem(&input); err != nil {
			t.Fatal(err)
		}
	}

	return ddb, endpoint
}
