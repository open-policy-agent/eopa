package batchquery_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/config"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/logging/test"
	"github.com/open-policy-agent/opa/v1/plugins"
	serverDecodingPlugin "github.com/open-policy-agent/opa/v1/plugins/server/decoding"
	"github.com/open-policy-agent/opa/v1/runtime"
	"github.com/open-policy-agent/opa/v1/server"
	"github.com/open-policy-agent/opa/v1/server/authorizer"
	"github.com/open-policy-agent/opa/v1/server/handlers"
	"github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/test/e2e"
	"github.com/open-policy-agent/opa/v1/util"
	"github.com/styrainc/enterprise-opa-private/pkg/batchquery"
)

type tr struct {
	method string
	path   string
	body   string // input
	code   int
	resp   string // output/result
}

// These tests are extracted from server_test.go's 'TestDataV1':
func TestBatchDataV1(t *testing.T) {
	t.Parallel()

	testMod1 := `package testmod

	import rego.v1
	import input.req1
	import input.req2 as reqx
	import input.req3.attr1

	p contains x if { q[x]; not r[x] }
	q contains x if { data.x.y[i] = x }
	r contains x if { data.x.z[i] = x }
	g = true if { req1.a[0] = 1; reqx.b[i] = 1 }
	h = true if { attr1[i] > 1 }
	gt1 = true if { req1 > 1 }
	arr = [1, 2, 3, 4] if { true }
	undef = true if { false }`

	// testMod2 := `package testmod
	// import rego.v1

	// p = [1, 2, 3, 4] if { true }
	// q = {"a": 1, "b": 2} if { true }`

	testMod4 := `package testmod
	import rego.v1

	p = true if { true }
	p = false if { true }`

	// testMod5 := `package testmod.empty.mod`
	// testMod6 := `package testmod.all.undefined
	// import rego.v1

	// p = true if { false }`
	testMod7 := `package testmod.condfail

	import rego.v1

	p[x] := v if {
		some i
		x := input.x[i]
		v := input.x[i] + input.y[i]
	}
		`

	tests := []struct {
		note string
		rego []byte
		reqs []tr
	}{
		{"post batch data all success", []byte(testMod1), []tr{
			{
				http.MethodPost, "/testmod/gt1",
				`{"inputs": {"AAA": {"req1": 2}, "BBB": {"req1": 3}, "CCC": {"req1": 4}}}`,
				200,
				`{"responses": {"AAA": {"result": true}, "BBB": {"result": true}, "CCC": {"result": true}}}`,
			},
		}},
		{"post batch data all failures", []byte(testMod4), []tr{
			{
				http.MethodPost, "/testmod/p",
				`{"inputs": { "AAA": {"x": 5}, "BBB": {"x": 5}, "CCC": {"x": 5} }}`,
				500,
				`{
				"responses": {
					"AAA":{"code":"internal_error","message":"test:5: eval_conflict_error: complete rules must not produce multiple outputs"},
					"BBB":{"code":"internal_error","message":"test:5: eval_conflict_error: complete rules must not produce multiple outputs"},
					"CCC":{"code":"internal_error","message":"test:5: eval_conflict_error: complete rules must not produce multiple outputs"}
				}
			}`,
			},
		}},
		{"post batch data mixed", []byte(testMod7), []tr{
			{
				http.MethodPost, "/testmod/condfail/p",
				`{"inputs": {
				"AAA": {"x": [1,1,3], "y": [1,1,1]},
				"BBB": {"x": [1,1,3], "y": [1,2,1]},
				"CCC": {"x": [1,1,3], "y": [1,1,1]}
			}}`,
				207,
				`{
				"responses": {
					"AAA":{"http_status_code": "200", "result": {"1":2,"3":4}},
					"BBB":{"http_status_code": "500", "code":"internal_error", "message":"test:5: eval_conflict_error: object keys must be unique"},
					"CCC":{"http_status_code": "200", "result": {"1":2,"3":4}}
				}
			}`,
			},
		}},
		{"post batch data strict-builtin-errors", []byte(`
			package test

			default p = false

			p if { 1/0 }
		`), []tr{
			{
				http.MethodPost, "/test/p?strict-builtin-errors",
				`{"inputs": { "AAA": {"x": {}} }}`,
				500,
				`{
				"responses": {
					"AAA":{"code": "internal_error", "message": "test:6: eval_builtin_error: div: divide by zero"}
				}
			}`,
			},
		}},
		{"post batch data api usage warning", []byte(testMod1), []tr{
			{http.MethodPost, "/test", "", 200, `{
				"warning": {
					"code": "api_usage_warning",
					"message": "'inputs' key missing from the request"
				}
			}`},
			{http.MethodPost, "/test", `{"inputs": {}}`, 200, `{}`},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			t.Parallel()

			bqhnd, _ := setup(t, tc.rego, map[string]any{}, nil, nil)
			for _, req := range tc.reqs {
				resp, _ := evalReq(t, bqhnd, req.path, req.code, []byte(req.body), nil)

				var exp batchquery.BatchDataResponseV1
				if err := json.Unmarshal([]byte(req.resp), &exp); err != nil {
					t.Fatalf("Unexpected JSON decode error: %v", err)
				}
				act := resp
				if diff := cmp.Diff(exp, act,
					cmpopts.IgnoreFields(batchquery.BatchDataResponseV1{}, "BatchDecisionID"),
					cmpopts.IgnoreFields(batchquery.DataResponseWithHTTPCodeV1{}, "DecisionID"),
					cmpopts.IgnoreFields(batchquery.ErrorResponseWithHTTPCodeV1{}, "DecisionID")); diff != "" {
					t.Fatalf("response: expected %v, got %v", exp, act)
				}
			}
		})
	}
}

// Ref: https://github.com/open-policy-agent/opa/issues/6804
func TestDataGetV1CompressedRequestWithAuthorizer(t *testing.T) {
	t.Parallel()

	authzPolicy := `package system.authz

	import rego.v1
	
	default allow := false # Reject requests by default.
	
	allow if {
		# Logic to authorize request goes here.
		input.body.user == "alice"
	}
	`

	tests := []struct {
		note                  string
		payload               []byte
		forcePayloadSizeField uint32 // Size to manually set the payload field for the gzip blob.
		expRespHTTPStatus     int
		expErrorMsg           string
	}{
		{
			note:              "empty message",
			payload:           mustGZIPPayload([]byte{}),
			expRespHTTPStatus: 401,
		},
		{
			note:              "empty object",
			payload:           mustGZIPPayload([]byte(`{}`)),
			expRespHTTPStatus: 401,
		},
		{
			note:              "basic authz - fail",
			payload:           mustGZIPPayload([]byte(`{"user": "bob"}`)),
			expRespHTTPStatus: 401,
		},
		{
			note:              "basic authz - pass",
			payload:           mustGZIPPayload([]byte(`{"user": "alice"}`)),
			expRespHTTPStatus: 200,
		},
		{
			note:                  "basic authz - malicious size field",
			payload:               mustGZIPPayload([]byte(`{"user": "alice"}`)),
			expRespHTTPStatus:     400,
			forcePayloadSizeField: 134217728, // 128 MB
			expErrorMsg:           "gzip: invalid checksum",
		},
		{
			note:              "basic authz - huge zip",
			payload:           mustGZIPPayload(util.MustMarshalJSON(generateJSONBenchmarkData(100, 100))),
			expRespHTTPStatus: 401,
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			t.Parallel()

			params := e2e.NewAPIServerTestParams()
			params.Authorization = server.AuthorizationBasic
			handler, _ := setup(t, []byte(authzPolicy), map[string]any{}, &params, nil)
			manager := handler.(batchquery.BatchQueryHandler).GetManager()

			// We have to emulate the "wrapper" handlers that are present in the
			// upstream OPA HTTP server.
			handler = wrapHandlerAuthz(handler, manager, server.AuthorizationBasic)
			handler, err := wrapHandlerDecodingLimits(handler, manager)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Forcibly replace the size trailer field for the gzip blob.
			// Byte order is little-endian, field is a uint32.
			if tc.forcePayloadSizeField != 0 {
				binary.LittleEndian.PutUint32(tc.payload[len(tc.payload)-4:], tc.forcePayloadSizeField)
			}

			// Execute the request against the batch API:
			if _, rr := evalReq(t, handler, "/test", tc.expRespHTTPStatus, tc.payload, map[string]string{"Content-Encoding": "gzip"}); err != nil {
				if tc.expErrorMsg != "" {
					var serverErr types.ErrorV1
					if err := json.Unmarshal(rr.Body.Bytes(), &serverErr); err != nil {
						t.Fatalf("Could not deserialize error message: %s", err.Error())
					}
					if serverErr.Message != tc.expErrorMsg {
						t.Fatalf("Expected error message to have message '%s', got message: '%s'", tc.expErrorMsg, serverErr.Message)
					}
				} else {
					t.Fatalf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestBatchDataMetrics(t *testing.T) {
	// These tests all use the POST /v1/batch/data API with ?metrics appended.
	// The original tests used the disk store to inject extra metrics, but we
	// never checked those for the Batch Query API, and so we're running on just
	// the in-memory store for these tests.

	testMod := `package a.b.c
	
	import rego.v1
	import data.x.y as z
	import data.p
	
	q contains x if { p[x]; not r[x] }
	r contains x if { z[x] = 4 }`

	bqhnd, _ := setup(t, []byte(testMod), map[string]any{}, nil, nil)

	// Make a request to evaluate `data`.
	testBatchDataMetrics(t, bqhnd, "?metrics", []byte(`{"inputs": {"A": {}}}`),
		[]string{
			"counter_server_query_cache_hit",
			"timer_rego_input_parse_ns",
			"timer_rego_query_compile_ns",
			"timer_rego_query_parse_ns",
			"timer_server_handler_ns",
		}, []string{
			"timer_rego_external_resolve_ns",
			"timer_rego_query_eval_ns",
			"timer_server_handler_ns",
		})

	// Repeat previous request, expect to have hit the query cache
	// so fewer timers should have been reported.
	testBatchDataMetrics(t, bqhnd, "?metrics", []byte(`{"inputs": {"A": {}}}`),
		[]string{
			"counter_server_query_cache_hit",
			"timer_rego_input_parse_ns",
			"timer_server_handler_ns",
		}, []string{
			"timer_rego_external_resolve_ns",
			"timer_rego_query_eval_ns",
			"timer_server_handler_ns",
		},
	)
}

func testBatchDataMetrics(t *testing.T, h http.Handler, path string, payload []byte, expectedGlobal []string, expectedQuery []string) {
	t.Helper()

	result, _ := evalReq(t, h, path, 200, payload, nil)
	assertMetricsExist(t, result.Metrics, expectedGlobal)

	for _, v := range result.Responses {
		if queryResponse, ok := v.(batchquery.DataResponseWithHTTPCodeV1); ok {
			assertMetricsExist(t, queryResponse.Metrics, expectedQuery)
		}
	}
}

func assertMetricsExist(t *testing.T, metrics types.MetricsV1, expected []string) {
	t.Helper()

	for _, key := range expected {
		v, ok := metrics[key]
		if !ok {
			t.Errorf("Missing expected metric: %s", key)
		} else if v == nil {
			t.Errorf("Expected non-nil value for metric: %s", key)
		}

	}

	if len(expected) != len(metrics) {
		t.Errorf("Expected %d metrics, got %d\n\n\tValues: %+v", len(expected), len(metrics), metrics)
	}
}

type counter struct {
	i atomic.Int32
}

func (c *counter) Next() string {
	n := c.i.Add(1)
	return strconv.Itoa(int(n))
}

func TestDecisionIDs(t *testing.T) {
	t.Parallel()

	testModFail := `package testmod

	p = true if { true }
	p = false if { true }`

	ctr := &counter{}
	ids := []string{}
	idsMutex := sync.Mutex{}

	tests := []struct {
		note string
		rego []byte
		reqs []tr
	}{
		// Note: Batch testcase can log decision IDs in arbitrary order.
		{"post batch data undefined rule", []byte(""), []tr{
			{
				http.MethodPost, "/undefined",
				`{"inputs": {"AAA": {}, "BBB": {}}}`,
				200,
				`{
				     "batch_decision_id": "1",
				     "responses": { "AAA": {"decision_id": "2"}, "BBB": {"decision_id": "3"} }
				}`,
			},
		}},
		// Ensure we get decision IDs, even for error cases.
		{"post batch data rule failures", []byte(testModFail), []tr{
			{
				http.MethodPost, "/testmod/p",
				`{"inputs": { "AAA": {"x": 5}, "BBB": {"x": 5} }}`,
				500,
				`{
					"batch_decision_id": "4",
					"responses": {
						"AAA":{"decision_id": "5", "code":"internal_error","message":"test:4: eval_conflict_error: complete rules must not produce multiple outputs"},
						"BBB":{"decision_id": "6", "code":"internal_error","message":"test:4: eval_conflict_error: complete rules must not produce multiple outputs"}
					}
				}`,
			},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			bqhnd, _ := setup(t, tc.rego, map[string]any{}, nil, nil)
			bqhnd = bqhnd.(batchquery.BatchQueryHandler).WithDecisionIDFactory(ctr.Next).WithDecisionLogger(func(_ context.Context, info *server.Info) error {
				idsMutex.Lock()
				defer idsMutex.Unlock()
				ids = append(ids, info.DecisionID)
				return nil
			})

			for _, req := range tc.reqs {
				resp, _ := evalReq(t, bqhnd, req.path, req.code, []byte(req.body), nil)

				var exp batchquery.BatchDataResponseV1
				if err := json.Unmarshal([]byte(req.resp), &exp); err != nil {
					t.Fatalf("Unexpected JSON decode error: %v", err)
				}
				act := resp
				if diff := cmp.Diff(exp, act,
					cmpopts.IgnoreFields(batchquery.BatchDataResponseV1{}, "BatchDecisionID"),
					cmpopts.IgnoreFields(batchquery.DataResponseWithHTTPCodeV1{}, "DecisionID"),
					cmpopts.IgnoreFields(batchquery.ErrorResponseWithHTTPCodeV1{}, "DecisionID")); diff != "" {
					t.Fatalf("response: expected %v, got %v", exp, act)
				}
			}
		})
	}

	// Batch IDs not included here, only decision IDs.
	exp := []string{"2", "3", "5", "6"}

	// Ensure the batch results match what we expect. Decision IDs in batch
	// operations are logged in arbitrary order, but are returned in a
	// deterministic order in the response to the caller.
	sortedIDs := slices.Clone(ids)
	slices.SortFunc(sortedIDs, func(a, b string) int {
		num1, _ := strconv.Atoi(a)
		num2, _ := strconv.Atoi(b)
		switch {
		case num1 < num2:
			return -1
		case num1 > num2:
			return 1
		default:
			return 0
		}
	})
	if !reflect.DeepEqual(sortedIDs, exp) {
		t.Fatalf("Expected %v but got %v", exp, sortedIDs)
	}
}

// Ensure JSON payload is compressed with gzip.
func mustGZIPPayload(payload []byte) []byte {
	var compressedPayload bytes.Buffer
	gz := gzip.NewWriter(&compressedPayload)
	if _, err := gz.Write(payload); err != nil {
		panic(fmt.Errorf("Error writing to gzip writer: %w", err))
	}
	if err := gz.Close(); err != nil {
		panic(fmt.Errorf("Error closing gzip writer: %w", err))
	}
	return compressedPayload.Bytes()
}

// generateJSONBenchmarkData returns a map of `k` keys and `v` key/value pairs.
// Taken from topdown/topdown_bench_test.go
func generateJSONBenchmarkData(k, v int) map[string]any {
	// create array of null values that can be iterated over
	keys := make([]any, k)
	for i := range keys {
		keys[i] = nil
	}

	// create large JSON object value (100,000 entries is about 2MB on disk)
	values := map[string]any{}
	for i := range v {
		values[fmt.Sprintf("key%d", i)] = fmt.Sprintf("value%d", i)
	}

	return map[string]any{
		"input": map[string]any{
			"keys":   keys,
			"values": values,
		},
	}
}

// params is optional. If nil, we use the default set.
func setup(t testing.TB, rego []byte, data any, params *runtime.Params, conf []byte) (http.Handler, *plugins.Manager) {
	ctx := context.Background()
	l := test.New()
	l.SetLevel(logging.Debug)
	// Generate default parameter set if not provided.
	if params == nil {
		defaultParams := e2e.NewAPIServerTestParams()
		params = &defaultParams
	}
	params.Logger = l
	trt, err := e2e.NewTestRuntime(*params)
	if err != nil {
		t.Fatalf("test runtime: %v", err)
	}
	t.Cleanup(trt.Cancel)

	if c, err := config.ParseConfig(conf, "batchquery_test"); err != nil {
		t.Fatalf("parse config: %v", err)
	} else {
		trt.Runtime.Manager.Config = c
		trt.Runtime.Manager.Init(ctx)
	}

	txn := storage.NewTransactionOrDie(ctx, trt.Runtime.Store, storage.WriteParams)
	if err := trt.Runtime.Store.UpsertPolicy(ctx, txn, "test", rego); err != nil {
		t.Fatalf("upsert policy: %v", err)
	}
	if data != nil {
		if err := trt.Runtime.Store.Write(ctx, txn, storage.AddOp, storage.Path{}, data); err != nil {
			t.Fatalf("write data: %v", err)
		}
	}
	if err := trt.Runtime.Store.Commit(ctx, txn); err != nil {
		t.Fatalf("commit: %v", err)
	}

	trt.Runtime.Manager.Info = ast.MustParseTerm(`{"foo": "bar", "fox": 100}`)
	bqhnd := batchquery.Handler(l)
	if err := bqhnd.SetManager(trt.Runtime.Manager); err != nil {
		t.Fatalf("set manager: %v", err)
	}

	return bqhnd, trt.Runtime.Manager
}

func evalReq(t testing.TB, h http.Handler, path string, code int, payload []byte, extraHeaders map[string]string) (batchquery.BatchDataResponseV1, *httptest.ResponseRecorder) {
	t.Helper()

	rr := evalReqMachinery(t, h, path, payload, extraHeaders)
	exp := code
	if act := rr.Code; exp != act {
		t.Errorf("response: %s", rr.Body.String())
		t.Fatalf("status code: expected %d, got %d", exp, act)
	}
	var resp batchquery.BatchDataResponseV1
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return resp, rr
}

// Shared logic between the Batch Query HTTP request functions used in the tests.
func evalReqMachinery(t testing.TB, h http.Handler, path string, payload []byte, extraHeaders map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	switch {
	case strings.HasPrefix(path, "/"), strings.HasPrefix(path, "?"):
		// Do nothing, path is fine as-is for templating.
	default:
		path = "/" + path
	}

	// CAVEAT(sr): We're using the httptest machinery to simulate a request, so the actual
	// request path is ignored.
	req := httptest.NewRequest("POST", fmt.Sprintf("/v1/batch/data%s", path), bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	// Mimic how the ExtraRoute calls will assign paths upsteam in the OPA server.
	router := http.NewServeMux()
	router.Handle("POST /v1/batch/data/{path...}", h)
	router.Handle("POST /v1/batch/data", h)
	router.ServeHTTP(rr, req)

	return rr
}

// Port of: func (s *Server) initHandlerDecodingLimits
func wrapHandlerDecodingLimits(handler http.Handler, manager *plugins.Manager) (http.Handler, error) {
	var decodingRawConfig json.RawMessage
	serverConfig := manager.Config.Server
	if serverConfig != nil {
		decodingRawConfig = serverConfig.Decoding
	}
	decodingConfig, err := serverDecodingPlugin.NewConfigBuilder().WithBytes(decodingRawConfig).Parse()
	if err != nil {
		return nil, err
	}
	decodingHandler := handlers.DecodingLimitsHandler(handler, *decodingConfig.MaxLength, *decodingConfig.Gzip.MaxLength)

	return decodingHandler, nil
}

// Port of: func (s *Server) initHandlerAuthz
func wrapHandlerAuthz(handler http.Handler, manager *plugins.Manager, scheme server.AuthorizationScheme) http.Handler {
	switch scheme {
	case server.AuthorizationBasic:
		handler = authorizer.NewBasic(
			handler,
			manager.GetCompiler,
			manager.Store,
			authorizer.Runtime(manager.Info),
			authorizer.Decision(manager.Config.DefaultAuthorizationDecisionRef),
			authorizer.PrintHook(manager.PrintHook()),
			authorizer.EnablePrintStatements(manager.EnablePrintStatements()),
			authorizer.URLPathExpectsBodyFunc(manager.ExtraAuthorizerRoutes()),
		)

		// Ignored, since this wrapper is for tests only. If metrics tests die, then we add it back
		// if bqhnd.metrics != nil {
		// 	handler = wrapHandlerInstrumentedMetrics(handler.ServeHTTP, PromHandlerAPIAuthz)
		// }
	}

	return handler
}

// Port of: func (s *Server) instrumentHandler
// func wrapHandlerInstrumentedMetrics(handler func(http.ResponseWriter, *http.Request), label string) http.Handler {
// 	var httpHandler http.Handler = http.HandlerFunc(handler)
// 	if len(s.distributedTracingOpts) > 0 {
// 		httpHandler = tracing.NewHandler(httpHandler, label, s.distributedTracingOpts)
// 	}
// 	if s.metrics != nil {
// 		return s.metrics.InstrumentHandler(httpHandler, label)
// 	}
// 	return httpHandler
// }
