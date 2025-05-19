package batchquery

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/server/authorizer"
)

func TestServerUsesAuthorizerParsedBody(t *testing.T) {
	t.Parallel()

	// Construct a request w/ a different message body (this should never happen.)
	req, err := http.NewRequest(http.MethodPost, "http://localhost:8182/v1/data/test/echo", bytes.NewBufferString(`{"foo": "bad"}`))
	if err != nil {
		t.Fatal(err)
	}

	testcases := []struct {
		Note             string
		AuthorizerCommon interface{}
		AuthorizerInputs map[string]interface{}
		ReqBody          string
		ExpResult        map[string]ast.Value
		ExpError         error
	}{
		{
			Note:      "No Inputs from Authorizer, No Req Body",
			ExpResult: map[string]ast.Value{},
		},
		{
			Note:    "Single Query Input from Req Body",
			ReqBody: `{"inputs": {"A": {"foo": "bad"}}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "bad"}`).Value,
			},
		},
		{
			Note:    "Single Query Input from Authorizer ",
			ReqBody: `{"inputs": {"A": {"foo": "bad"}}}`,
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
			},
		},
		{
			Note: "Multiple Query Inputs from Authorizer",
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
				"B": map[string]interface{}{"bar": "bad"},
				"C": map[string]interface{}{"baz": "questionable"},
			},
			ReqBody: `{"inputs": {"A": {"foo": "bad"}}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`{"bar": "bad"}`).Value,
				"C": ast.MustParseTerm(`{"baz": "questionable"}`).Value,
			},
		},
		{
			Note:    "Multiple Query Inputs from Req Body",
			ReqBody: `{"inputs": {"A": {"foo": "good"}, "B": {"bar": "bad"}, "C": {"baz": "questionable"}}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`{"bar": "bad"}`).Value,
				"C": ast.MustParseTerm(`{"baz": "questionable"}`).Value,
			},
		},
		// Common input from request examples. Should be no difference from authorizer results.
		{
			Note:    "Common Input from Request, Single Query Input from Req Body",
			ReqBody: `{"inputs": {"A": {"foo": "bad"}}, "common_input": {"wow": "wee"}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "bad", "wow": "wee"}`).Value,
			},
		},
		{
			Note:    "Common Input from Request, Single Query Input from Authorizer ",
			ReqBody: `{"inputs": {"A": {"foo": "bad"}}, "common_input": {"wow": "wee"}}`,
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
			},
		},
		{
			Note: "Common Input from Request, Multiple Query Inputs from Authorizer",
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
				"B": map[string]interface{}{"bar": "bad"},
				"C": map[string]interface{}{"baz": "questionable"},
			},
			ReqBody: `{"inputs": {"A": {"foo": "bad"}}, "common_input": {"wow": "wee"}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`{"bar": "bad"}`).Value,
				"C": ast.MustParseTerm(`{"baz": "questionable"}`).Value,
			},
		},
		{
			Note:    "Common Input from Request, Multiple Query Inputs from Req Body",
			ReqBody: `{"inputs": {"A": {"foo": "good"}, "B": {"bar": "bad"}, "C": {"baz": "questionable"}}, "common_input": {"wow": "wee"}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good", "wow": "wee"}`).Value,
				"B": ast.MustParseTerm(`{"bar": "bad", "wow": "wee"}`).Value,
				"C": ast.MustParseTerm(`{"baz": "questionable", "wow": "wee"}`).Value,
			},
		},
		// Common input from authorizer. We want to see correct merging behavior here.
		{
			Note:             "Common Input from Authorizer, No Inputs from Authorizer, No Req Body",
			AuthorizerCommon: map[string]interface{}{"wow": "wee"},
			ExpResult:        map[string]ast.Value{},
		},
		{
			Note:             "Common Input from Authorizer, Single Query Input from Req Body",
			ReqBody:          `{"inputs": {"A": {"foo": "bad"}}}`,
			AuthorizerCommon: map[string]interface{}{"wow": "wee"},
			ExpResult:        map[string]ast.Value{}, // Should be an impossible case, but Authorizer path always wins.
		},
		{
			Note:             "Common Input from Authorizer, Single Query Input from Authorizer ",
			ReqBody:          `{"inputs": {"A": {"foo": "bad"}}}`,
			AuthorizerCommon: map[string]interface{}{"wow": "wee"},
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good", "wow": "wee"}`).Value,
			},
		},
		{
			Note:             "Common Input from Authorizer, Multiple Query Inputs from Authorizer",
			AuthorizerCommon: map[string]interface{}{"wow": "wee"},
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
				"B": map[string]interface{}{"bar": "bad"},
				"C": map[string]interface{}{"baz": "questionable"},
			},
			ReqBody: `{"inputs": {"A": {"foo": "bad"}}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good", "wow": "wee"}`).Value,
				"B": ast.MustParseTerm(`{"bar": "bad", "wow": "wee"}`).Value,
				"C": ast.MustParseTerm(`{"baz": "questionable", "wow": "wee"}`).Value,
			},
		},
		{
			Note:             "Common Input from Authorizer, Multiple Query Inputs from Req Body",
			ReqBody:          `{"inputs": {"A": {"foo": "good"}, "B": {"bar": "bad"}, "C": {"baz": "questionable"}}}`,
			AuthorizerCommon: map[string]interface{}{"wow": "wee"},
			ExpResult:        map[string]ast.Value{}, // Should be an impossible case, but Authorizer path always wins.
		},
		// Type mis-match tests for Authorizer case. (The object + object case is already tested by earlier testcaes.)
		{
			Note:             "Authorizer, common array, inputs other",
			AuthorizerCommon: []int{1, 2, 3},
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
				"B": []string{"A", "B", "C"},
				"C": 27,
				"D": "X",
				"E": true,
				"F": nil,
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		{
			Note:             "Authorizer, common string, inputs other",
			AuthorizerCommon: "ZZZ",
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
				"B": []string{"A", "B", "C"},
				"C": 27,
				"D": "X",
				"E": true,
				"F": nil,
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		{
			Note:             "Authorizer, common bool, inputs other",
			AuthorizerCommon: false,
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
				"B": []string{"A", "B", "C"},
				"C": 27,
				"D": "X",
				"E": true,
				"F": nil,
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		{
			Note:             "Authorizer, common number, inputs other",
			AuthorizerCommon: 42,
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
				"B": []string{"A", "B", "C"},
				"C": 27,
				"D": "X",
				"E": true,
				"F": nil,
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		{
			Note:             "Authorizer, common null, inputs other",
			AuthorizerCommon: nil,
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
				"B": []string{"A", "B", "C"},
				"C": 27,
				"D": "X",
				"E": true,
				"F": nil,
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		// Type mis-match tests for Request body case. (The object + object case is already tested by earlier testcaes.)
		{
			Note:    "Request, common array, inputs other",
			ReqBody: `{"inputs": {"A": {"foo": "good"}, "B": ["A", "B", "C"], "C": 27, "D": "X", "E": true, "F": null}, "common_input": [1, 2, 3]}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		{
			Note:    "Request, common string, inputs other",
			ReqBody: `{"inputs": {"A": {"foo": "good"}, "B": ["A", "B", "C"], "C": 27, "D": "X", "E": true, "F": null}, "common_input": "ZZZ"}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		{
			Note:    "Request, common bool, inputs other",
			ReqBody: `{"inputs": {"A": {"foo": "good"}, "B": ["A", "B", "C"], "C": 27, "D": "X", "E": true, "F": null}, "common_input": false}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		{
			Note:    "Request, common number, inputs other",
			ReqBody: `{"inputs": {"A": {"foo": "good"}, "B": ["A", "B", "C"], "C": 27, "D": "X", "E": true, "F": null}, "common_input": 42}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		{
			Note:    "Request, common null, inputs other",
			ReqBody: `{"inputs": {"A": {"foo": "good"}, "B": ["A", "B", "C"], "C": 27, "D": "X", "E": true, "F": null}, "common_input": null}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
				"B": ast.MustParseTerm(`["A", "B", "C"]`).Value,
				"C": ast.MustParseTerm(`27`).Value,
				"D": ast.MustParseTerm(`"X"`).Value,
				"E": ast.MustParseTerm(`true`).Value,
				"F": ast.MustParseTerm(`null`).Value,
			},
		},
		// Conflicting keys between inputs and common input.
		{
			Note:    "Top-level key conflict, request",
			ReqBody: `{"inputs": {"A": {"foo": "good"}}, "common_input": {"foo": "bad"}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
			},
		},
		{
			Note:             "Top-level key conflict, authorizer",
			AuthorizerCommon: map[string]interface{}{"foo": "bad"},
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": "good"},
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": "good"}`).Value,
			},
		},
		// Top-level matching keys recursively merge.
		{
			Note:    "Top-level key conflict, request, recursive merge",
			ReqBody: `{"inputs": {"A": {"foo": {"1a": 1, "2a": 2}}}, "common_input": {"foo": {"3a": 3}}}`,
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": {"1a": 1, "2a": 2, "3a": 3}}`).Value,
			},
		},
		{
			Note: "Top-level key conflict, authorizer, recursive merge",
			AuthorizerCommon: map[string]interface{}{
				"foo": map[string]interface{}{"3b": 3},
			},
			AuthorizerInputs: map[string]interface{}{
				"A": map[string]interface{}{"foo": map[string]interface{}{
					"1b": 1,
					"2b": 2,
				}},
			},
			ExpResult: map[string]ast.Value{
				"A": ast.MustParseTerm(`{"foo": {"1b": 1, "2b": 2, "3b": 3}}`).Value,
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.Note, func(t *testing.T) {
			ctx := req.Context()

			// Check that v1 Batch reader function behaves correctly.
			// Construct a request w/ a different message body (this should never happen.)
			reqBatch, err := http.NewRequest(http.MethodPost, "http://localhost:8182/v1/batch/data/test/echo", bytes.NewBufferString(tc.ReqBody))
			if err != nil {
				t.Fatal(err)
			}

			// Set the authorizer's parsed input to the expected message body.
			if tc.AuthorizerInputs != nil || tc.AuthorizerCommon != nil {
				authorizerBody := map[string]any{}
				if tc.AuthorizerInputs != nil {
					authorizerBody["inputs"] = tc.AuthorizerInputs
				}
				if tc.AuthorizerCommon != nil {
					authorizerBody["common_input"] = tc.AuthorizerCommon
				}
				ctx = authorizer.SetBodyOnContext(req.Context(), authorizerBody)
			}

			// Check that v1 reader function behaves correctly.
			inpBatch, goInpBatch, err := readInputBatchPostV1(reqBatch.WithContext(ctx))
			if err != nil {
				t.Fatal(err)
			}

			expBatch := tc.ExpResult

			for k, v := range expBatch {
				if v.Compare(inpBatch[k]) != 0 {
					t.Fatalf("expected %v but got %v", expBatch[k], inpBatch[k])
				}
			}

			for k, v := range expBatch {
				if v.Compare(ast.MustInterfaceToValue(goInpBatch[k])) != 0 {
					t.Fatalf("expected %v but got %v", expBatch[k], goInpBatch[k])
				}
			}
		})
	}
}
