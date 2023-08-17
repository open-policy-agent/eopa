package preview

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

const previewConfig string = `{"plugins":{"preview":{}}}`

var testPolicy = map[string]string{
	"test/test.rego": `package test

	direct := true
	existingData = result {
		result := data.loaded.test
	}
	previewData = result {
		result := data.preview.test
	}
	inputData = result {
		print("testing123")
		result := input.test
	}`,
}

var testData bjson.Json = bjson.MustNew(map[string]any{
	"loaded": map[string]any{
		"test": "preloaded data",
	},
})

func TestMethods(t *testing.T) {
	testCases := []struct {
		method string
		code   int
	}{
		{
			method: http.MethodGet,
			code:   http.StatusMethodNotAllowed,
		},
		{
			method: http.MethodPost,
			code:   http.StatusOK,
		},
		{
			method: http.MethodPut,
			code:   http.StatusMethodNotAllowed,
		},
		{
			method: http.MethodPatch,
			code:   http.StatusMethodNotAllowed,
		},
		{
			method: http.MethodDelete,
			code:   http.StatusMethodNotAllowed,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			ctx := context.Background()
			manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
			manager.Start(ctx)

			w := httptest.NewRecorder()
			r := httptest.NewRequest(tc.method, "/v0/preview/test", nil)
			manager.GetRouter().ServeHTTP(w, r)

			if w.Code != tc.code {
				t.Fatalf("Expected http status %d but received %d", tc.code, w.Code)
			}
		})
	}
}

func TestHttpIO(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()

	testCases := []struct {
		name     string
		body     json.RawMessage
		response map[string]any
	}{
		{
			name:     "basic request",
			body:     nil,
			response: map[string]any{"direct": true, "existingData": "preloaded data"},
		},
		{
			name:     "basic with input",
			body:     []byte(`{"input": {"test": "input data"}}`),
			response: map[string]any{"direct": true, "existingData": "preloaded data", "inputData": "input data"},
		},
		{
			name:     "with preview data",
			body:     []byte(`{"data": {"preview": {"test": "preview data"}}}`),
			response: map[string]any{"direct": true, "existingData": "preloaded data", "previewData": "preview data"},
		},
		{
			name:     "with preview policy",
			body:     []byte(`{"rego_modules": {"test/extra.rego": "package test\n\nextraPolicy:=\"from preview policy\""}}`),
			response: map[string]any{"direct": true, "existingData": "preloaded data", "extraPolicy": "from preview policy"},
		},
		{
			name:     "with override data",
			body:     []byte(`{"data": {"loaded": {"test": "overloaded data"}}}`),
			response: map[string]any{"direct": true, "existingData": "overloaded data"},
		},
		{
			name:     "with override policy",
			body:     []byte(`{"rego_modules": {"test/test.rego": "package test\n\ndirect:=\"overridden policy\""}}`),
			response: map[string]any{"direct": "overridden policy"},
		},
		{
			name: "ad hoc query",
			body: []byte(`{"rego": "direct := true"}`),
			response: map[string]any{
				"expressions": []any{
					map[string]any{
						"value": true,
						"text":  "direct := true",
						"location": map[string]any{
							"row": float64(1),
							"col": float64(1),
						},
					},
				},
				"bindings": map[string]any{
					"direct": true,
				},
			},
		},
		{
			name: "nd builtin cache",
			body: []byte(`{
				"rego_modules": {
					"test/test.rego": "package test\ndirect:= result {\nrequest = http.send({\"method\":\"GET\", \"url\": \"https://example.com/todos/1\"})\nresult:=request.body\n}"
				},
				"nd_builtin_cache": {
					"http.send": {
						"[{\"method\":\"GET\", \"url\": \"https://example.com/todos/1\"}]": {
							"body": { "from_cache": true }
						}
					}
				}
			}`),
			response: map[string]any{"direct": map[string]any{"from_cache": true}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			var body io.Reader
			if tc.body != nil {
				body = bytes.NewBuffer(tc.body)
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", body)
			router.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
			}

			var value map[string]any
			json.NewDecoder(w.Body).Decode(&value)
			if diff := cmp.Diff(map[string]any{"result": tc.response}, value); diff != "" {
				t.Errorf("Unexpected response body (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestInstrumentationMetrics(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()

	testCases := []struct {
		name string
		keys []string
	}{
		{
			name: "metrics",
			keys: []string{
				"timer_rego_query_eval_ns",
				"timer_rego_query_parse_ns",
				"timer_server_handler_ns",
				"timer_rego_external_resolve_ns",
				"timer_rego_input_parse_ns",
				"timer_rego_query_compile_ns",
			},
		},
		{
			name: "instrument",
			keys: []string{
				"timer_rego_query_eval_ns",
				"timer_rego_query_parse_ns",
				"timer_server_handler_ns",
				"timer_rego_external_resolve_ns",
				"timer_rego_input_parse_ns",
				"timer_rego_query_compile_ns",
				"timer_eval_op_rule_index_ns",
				"counter_eval_op_base_cache_hit",
				"timer_eval_op_resolve_ns",
				"timer_eval_op_plug_ns",
				"timer_eval_op_builtin_call_ns",
				"counter_eval_op_comprehension_cache_miss",
				"histogram_eval_op_builtin_call",
				"counter_eval_op_comprehension_cache_skip",
				"timer_query_compile_stage_check_undefined_funcs_ns",
				"timer_query_compile_stage_check_unsafe_builtins_ns",
				"timer_query_compile_stage_rewrite_dynamic_terms_ns",
				"timer_query_compile_stage_rewrite_to_capture_value_ns",
				"timer_query_compile_stage_rewrite_comprehension_terms_ns",
				"timer_query_compile_stage_rewrite_print_calls_ns",
				"counter_eval_op_base_cache_miss",
				"counter_eval_op_virtual_cache_miss",
				"histogram_eval_op_plug",
				"timer_query_compile_stage_check_keyword_overrides_ns",
				"timer_query_compile_stage_check_types_ns",
				"histogram_eval_op_resolve",
				"histogram_eval_op_rule_index",
				"timer_query_compile_stage_build_comprehension_index_ns",
				"timer_query_compile_stage_rewrite_local_vars_ns",
				"timer_query_compile_stage_rewrite_with_values_ns",
				"timer_query_compile_stage_check_deprecated_builtins_ns",
				"timer_query_compile_stage_check_safety_ns",
				"timer_query_compile_stage_check_void_calls_ns",
				"timer_query_compile_stage_resolve_refs_ns",
				"timer_query_compile_stage_rewrite_expr_terms_ns",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", nil)
			query := r.URL.Query()
			query.Add(tc.name, "true")
			r.URL.RawQuery = query.Encode()
			router.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
			}

			var value map[string]any
			json.NewDecoder(w.Body).Decode(&value)

			var metrics map[string]any
			if m, ok := value["metrics"]; !ok {
				t.Fatalf("Metrics or instrumentation was requested but the response did not contain them")
			} else {
				if metrics, ok = m.(map[string]any); !ok {
					t.Fatalf("Metrics was not a map[string]any: %v", m)
				}
			}

			expectedKeys := make(map[string]bool, len(tc.keys))
			for _, key := range tc.keys {
				expectedKeys[key] = false
			}
			for key := range metrics {
				if _, ok := expectedKeys[key]; !ok {
					t.Errorf("Found %q in the metrics keys but was not expecting to", key)
				} else {
					expectedKeys[key] = true
				}
			}
			for key, seen := range expectedKeys {
				if !seen {
					t.Errorf("Expected to find %q in metrics, but the key is not present", key)
				}
			}
		})
	}
}

func TestProvenance(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v0/preview/test?provenance", nil)

	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
	}

	var value map[string]any
	json.NewDecoder(w.Body).Decode(&value)

	if _, ok := value["provenance"]; !ok {
		t.Fatalf("Provenance was requested but the response did not contain it")
	}
}

func TestPrint(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v0/preview/test?print=", bytes.NewBufferString(`{"input": {"test": "input data"}}`))

	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
	}

	var value map[string]any
	json.NewDecoder(w.Body).Decode(&value)

	expected := "testing123\n"
	if printed, ok := value["printed"]; !ok {
		t.Fatalf("Printed output was requested but the response did not contain it")
	} else if expected != printed {
		t.Errorf("Expected %q to be printed, but received %q", expected, printed)
	}
}

func TestSandbox(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()

	body := bytes.NewBufferString(`{
		"data": {
			"preview": {
				"test": "from test data"
			}
		},
		"rego_modules": {
			"test/extra.rego": "package test\n\nextraPolicy:=\"from preview policy\"\nexisting:=data.loaded.test\nadded:=data.preview.test"
		}}
	}`)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v0/preview/test?sandbox", body)

	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
	}

	// Note: the existing data is not present in sandbox mode, so 'existing' is not returned
	expectedResponse := map[string]any{
		"extraPolicy": "from preview policy",
		"added":       "from test data",
	}
	var value map[string]any
	json.NewDecoder(w.Body).Decode(&value)
	if diff := cmp.Diff(map[string]any{"result": expectedResponse}, value); diff != "" {
		t.Errorf("Unexpected response body (-want, +got):\n%s", diff)
	}
}

func TestStrictBuiltinErrors(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()

	body := `{
		"rego_modules": {
			"test/test.rego": "package test\ndirect= result {\nrequest := http.send({})\nresult:=request.body\n}"
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", bytes.NewBufferString(body))

	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected http status %d without built-in errors flag but received %d", http.StatusOK, w.Code)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/v0/preview/test?strict-builtin-errors=true", bytes.NewBufferString(body))

	router.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected http status %d with built-in errors flag but received %d", http.StatusInternalServerError, w.Code)
	}
}

func TestPrettyReturn(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", nil)

	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
	}

	body, err := io.ReadAll(w.Body)
	if err != nil {
		t.Fatalf("Could not read response body: %v", err)
	}

	expectedWithoutPretty := []byte("{\"result\":{\"direct\":true,\"existingData\":\"preloaded data\"}}\n")
	if !bytes.Equal(body, expectedWithoutPretty) {
		t.Errorf("Unexpected output without the pretty argument:\nexpected: %q\nreceived: %q", string(expectedWithoutPretty), string(body))
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/v0/preview/test?pretty", nil)

	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
	}

	body, err = io.ReadAll(w.Body)
	if err != nil {
		t.Fatalf("Could not read response body: %v", err)
	}

	expectedWithPretty := []byte("{\n  \"result\": {\n    \"direct\": true,\n    \"existingData\": \"preloaded data\"\n  }\n}\n")
	if !bytes.Equal(body, expectedWithPretty) {
		t.Errorf("Unexpected output with the pretty argument:\nexpected: %q\nreceived: %q", string(expectedWithPretty), string(body))
	}
}

func TestStrictCompile(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()

	body := `{
		"rego_modules": {
			"test/test.rego": "package test\nimport future.keywords\nimport future.keywords\nadditional := true"
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", bytes.NewBufferString(body))
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/v0/preview/test?strict", bytes.NewBufferString(body))
	router.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected http status %d but received %d", http.StatusInternalServerError, w.Code)
	}
}

func TestBodyFormat(t *testing.T) {
	ctx := context.Background()
	manager := pluginMgr(ctx, t, testPolicy, testData, previewConfig)
	manager.Start(ctx)
	router := manager.GetRouter()
	expected := map[string]any{"direct": true, "existingData": "preloaded data", "inputData": "input data"}

	testCases := []struct {
		name       string
		body       []byte
		headers    map[string]string
		compressed bool
	}{
		{
			name:       "JSON",
			body:       []byte(`{"input": {"test": "input data"}}`),
			headers:    map[string]string{"Content-Type": "application/json"},
			compressed: false,
		},
		{
			name:       "GZipped JSON",
			body:       []byte(`{"input": {"test": "input data"}}`),
			headers:    map[string]string{"Content-Type": "application/json", "Content-Encoding": "gzip"},
			compressed: true,
		},
		{
			name:       "YAML",
			body:       []byte("input:\n  test: input data"),
			headers:    map[string]string{"Content-Type": "application/yaml"},
			compressed: false,
		},
		{
			name:       "GZipped YAML",
			body:       []byte("input:\n  test: input data"),
			headers:    map[string]string{"Content-Type": "application/yaml", "Content-Encoding": "gzip"},
			compressed: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.compressed {
				var buf bytes.Buffer
				gz := gzip.NewWriter(&buf)
				_, err := gz.Write(tc.body)
				if err != nil {
					t.Fatalf("Unable to write compressed body: %v", err)
				}
				gz.Close()
				body = &buf
			} else {
				body = bytes.NewBuffer(tc.body)
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/v0/preview/test", body)
			for key, value := range tc.headers {
				r.Header.Add(key, value)
			}
			router.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected http status %d but received %d", http.StatusOK, w.Code)
			}

			var value map[string]any
			json.NewDecoder(w.Body).Decode(&value)
			if diff := cmp.Diff(map[string]any{"result": expected}, value); diff != "" {
				t.Errorf("Unexpected response body (-want, +got):\n%s", diff)
			}
		})
	}
}
