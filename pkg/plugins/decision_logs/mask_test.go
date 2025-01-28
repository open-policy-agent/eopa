package decisionlogs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMaskRule(t *testing.T) {
	tests := []struct {
		note, event, exp, mask string
	}{
		{
			note:  `erase input`,
			event: `{"input": {"a": 1}}`,
			exp:   `{"erased": ["/input"]}`,
			mask:  `{"op":"remove","path":"/input","value":null}`,
		},
		{
			note:  `upsert input`,
			event: `{"input": {"a": 1}}`,
			exp:   `{"masked": ["/input"], "input": {"RandoString": "foo"}}`,
			mask:  `{"op":"upsert","path":"/input","value":{"RandoString":"foo"}}`,
		},
		{
			note:  `erase result`,
			event: `{"result": "foo"}`,
			exp:   `{"erased": ["/result"]}`,
			mask:  `{"op":"remove","path":"/result","value":null}`,
		},
		{
			note:  `upsert result`,
			event: `{"result": "foo"}`,
			exp:   `{"masked": ["/result"], "result": "upserted"}`,
			mask:  `{"op":"upsert","path":"/result","value":"upserted"}`,
		},
		{
			note:  `upsert: array: object value by index`,
			event: `{"input": {"foo": [{"x": "y"}, {"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"x": "y"}, {"baz": 2}]}, "masked":["/input/foo/1/baz"]}`,
			mask:  `{"op":"upsert","path":"/input/foo/1/baz","value":2}`,
		},
		{
			note:  `upsert: array: element by index`,
			event: `{"input": {"foo": [{"x": "y"}, {"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"x": "y"}, {"bar": 3}]}, "masked":["/input/foo/1"]}`,
			mask:  `{"op":"upsert","path":"/input/foo/1","value": {"bar": 3}}`,
		},
		{
			note:  `erase: array: element by index`,
			event: `{"input": {"foo": [{"x": "y"}, {"baz": 1}]}}`,
			exp:   `{"input": {"foo": [null, {"baz": 1}]}, "masked": ["/input/foo/0"]}`,
			mask:  `{"op":"upsert","path":"/input/foo/0","value":null}`,
		},
		{
			note:  `erase: array: object value by index`,
			event: `{"input": {"foo": [{"x": "y"}, {"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"x": "y"}, {"baz": null}]}, "masked":["/input/foo/1/baz"]}`,
			mask:  `{"op":"upsert","path":"/input/foo/1/baz","value":null}`,
		},
		{
			note:  `erase undefined input`,
			event: `{}`,
			exp:   `{}`,
			mask:  `{"op":"remove","path":"/input/foo","value":null}`,
		},
		{
			note:  `erase undefined input: fail unknown object path on`,
			event: `{}`,
			exp:   `{}`,
			mask:  `{"op":"remove","path":"/input/foo","value":null}`,
		},
		{
			note:  `upsert undefined input`,
			event: `{}`,
			exp:   `{}`,
			mask:  `{"op":"upsert","path":"/input/foo","value":null}`,
		},
		{
			note:  `upsert undefined input: fail unknown object path on`,
			event: `{}`,
			exp:   `{}`,
			mask:  `{"op":"upsert","path":"/input/foo","value":null}`,
		},
		{
			note:  `erase undefined result`,
			event: `{}`,
			exp:   `{}`,
			mask:  `{"op":"remove","path":"/result/foo","value":null}`,
		},
		{
			note:  `erase undefined result: fail unknown object path on`,
			event: `{}`,
			exp:   `{}`,
			mask:  `{"op":"remove","path":"/result/foo","value":null}`,
		},
		{
			note:  `upsert undefined result`,
			event: `{}`,
			exp:   `{}`,
			mask:  `{"op":"upsert","path":"/result/foo","value":null}`,
		},
		{
			note:  `upsert undefined result: fail unknown object path on`,
			event: `{}`,
			exp:   `{}`,
			mask:  `{"op":"upsert","path":"/result/foo","value":null}`,
		},
		{
			note:  `erase undefined node`,
			event: `{"input": {"bar": 1}}`,
			exp:   `{"input": {"bar": 1}}`,
			mask:  `{"op":"remove","path":"/input/foo","value":null}`,
		},
		{
			note:  `erase undefined node: fail unknown object path on`,
			event: `{"input": {"bar": 1}}`,
			exp:   `{"input": {"bar": 1}}`,
			mask:  `{"op":"remove","path":"/input/foo","value":null}`,
		},
		{
			note:  `upsert undefined node with nil value`,
			event: `{"input": {"bar": 1}}`,
			exp:   `{"input": {"bar": 1, "foo": null}, "masked": ["/input/foo"]}`,
			mask:  `{"op":"upsert","path":"/input/foo","value":null}`,
		},
		{
			note:  `upsert undefined node with nil value: fail unknown object path on`,
			event: `{"input": {"bar": 1}}`,
			exp:   `{"input": {"bar": 1, "foo": null}, "masked": ["/input/foo"]}`,
			mask:  `{"op":"upsert","path":"/input/foo","value":null}`,
		},
		{
			note:  `upsert undefined node with a value`,
			event: `{"input": {"bar": 1}}`,
			exp:   `{"input": {"bar": 1, "foo": "upserted"}, "masked": ["/input/foo"]}`,
			mask:  `{"op":"upsert","path":"/input/foo","value":"upserted"}`,
		},
		{
			note:  `erase undefined node-2`,
			event: `{"input": {"foo": 1}}`,
			exp:   `{"input": {"foo": 1}}`,
			mask:  `{"op":"remove","path":"/input/foo/bar","value":null}`,
		},
		{
			note:  `upsert unsupported nested object type (json.Number) #1`,
			event: `{"input": {"foo": 1}}`,
			exp:   `{"input": {"foo": 1}}`,
			mask:  `{"op":"upsert","path":"/input/foo/bar","value":null}`,
		},
		{
			note:  `upsert unsupported nested object type (string) #1`,
			event: `{"input": {"foo": "bar"}}`,
			exp:   `{"input": {"foo": "bar"}}`,
			mask:  `{"op":"upsert","path":"/input/foo/bar","value":null}`,
		},
		{
			note:  `erase: undefined object: missing key`,
			event: `{"input": {"foo": {}}}`,
			exp:   `{"input": {"foo": {}}}`,
			mask:  `{"op":"remove","path":"/input/foo/bar/baz","value":null}`,
		},
		{
			note:  `upsert: undefined object: missing key, no value`,
			event: `{"input": {"foo": {}}}`,
			exp:   `{"input": {"foo": {"bar": {"baz": null}}}, "masked": ["/input/foo/bar/baz"]}`,
			mask:  `{"op":"upsert","path":"/input/foo/bar/baz","value":null}`,
		},
		{
			note:  `upsert: undefined object: missing key, provided value`,
			event: `{"input": {"foo": {}}}`,
			exp:   `{"input": {"foo": {"bar": {"baz": 100}}}, "masked": ["/input/foo/bar/baz"]}`,
			mask:  `{"op":"upsert","path":"/input/foo/bar/baz","value":100}`,
		},
		{
			note:  `erase: undefined scalar`,
			event: `{"input": {"foo": 1}}`,
			exp:   `{"input": {"foo": 1}}`,
			mask:  `{"op":"remove","path":"/input/foo/bar/baz","value":null}`,
		},
		{
			note:  `upsert: unsupported nested object type (json.Number) #2`,
			event: `{"input": {"foo": 1}}`,
			exp:   `{"input": {"foo": 1}}`,
			mask:  `{"op":"upsert","path":"/input/foo/bar/baz","value":null}`,
		},
		{
			note:  `erase: undefined array: non-int index`,
			event: `{"input": {"foo": [{"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"baz": 1}]}}`,
			mask:  `{"op":"remove","path":"/input/foo/bar/baz","value":null}`,
		},
		{
			note:  `upsert: unsupported type: []interface {}`,
			event: `{"input": {"foo": [{"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"baz": 1}]}}`,
			mask:  `{"op":"upsert","path":"/input/foo/bar/baz","value":null}`,
		},
		{
			note:  `erase: undefined array: negative index`,
			event: `{"input": {"foo": [{"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"baz": 1}]}}`,
			mask:  `{"op":"remove","path":"/input/foo/-1/baz","value":null}`,
		},
		{
			note:  `upsert: undefined array: negative index`,
			event: `{"input": {"foo": [{"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"baz": 1}]}}`,
			mask:  `{"op":"upsert","path":"/input/foo/-1/baz","value":null}`,
		},
		{
			note:  `erase: undefined array: index out of range`,
			event: `{"input": {"foo": [{"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"baz": 1}]}}`,
			mask:  `{"op":"remove","path":"/input/foo/1/baz","value":null}`,
		},
		{
			note:  `upsert: unsupported nested object type (array) #1`,
			event: `{"input": {"foo": [{"baz": 1}]}}`,
			exp:   `{"input": {"foo": [{"baz": 1}]}}`,
			mask:  `{"op":"upsert","path":"/input/foo/1/baz","value":null}`,
		},
		{
			note:  `erase: undefined array: remove element`,
			event: `{"input": {"foo": [1]}}`,
			// exp:   `{"input": {"foo": [1]}}`, // TODO(sr): OPA case seemed wrong
			exp:  `{"input": {"foo": []}, "erased": ["/input/foo/0"]}`,
			mask: `{"op":"remove","path":"/input/foo/0","value":null}`,
		},
		{
			note:  `upsert: unsupported nested object type (array) #2`,
			event: `{"input": {"foo": [1]}}`,
			// exp:   `{"input": {"foo": [1]}}`, // TODO(sr): OPA case seemed wrong
			exp:  `{"input": {"foo": [null]}, "masked": ["/input/foo/0"]}`,
			mask: `{"op":"upsert","path":"/input/foo/0","value":null}`,
		},
		{
			note:  `erase: object key`,
			event: `{"input": {"bar": 1, "foo": [{"baz": 1}]}}`,
			exp:   `{"input": {"bar": 1}, "erased": ["/input/foo"]}`,
			mask:  `{"op":"remove","path":"/input/foo","value":null}`,
		},
		{
			note:  `upsert: object key`,
			event: `{"input": {"bar": 1, "foo": [{"baz": 1}]}}`,
			exp:   `{"input": {"bar": 1, "foo": [{"nabs": 1}]}, "masked": ["/input/foo"]}`,
			mask:  `{"op":"upsert","path":"/input/foo","value":[{"nabs":1}]}`,
		},
		{
			note:  `erase: object key (multiple)`,
			event: `{"input": {"bar": 1}, "erased": ["/input/foo"]}`,
			exp:   `{"input": {}, "erased": ["/input/foo", "/input/bar"]}`,
			mask:  `{"op":"remove","path":"/input/bar","value":null}`,
		},
		{
			note:  `erase: object key (nested array)`,
			event: `{"input": {"foo": [{"bar": 1, "baz": 2}]}}`,
			exp:   `{"input": {"foo": [{"baz": 2}]}, "erased": ["/input/foo/0/bar"]}`,
			mask:  `{"op":"remove","path":"/input/foo/0/bar","value":null}`,
		},
		{
			note:  `erase input: special character in path`,
			event: `{"input": {"bar": 1, ":path": "token"}}`,
			exp:   `{"input": {"bar": 1}, "erased": ["/input/:path"]}`,
			mask:  `{"op":"remove","path":"/input/:path","value":null}`,
		},
		{
			note:  `upsert input: special character in path`,
			event: `{"input": {"bar": 1, ":path": "token"}}`,
			exp:   `{"input": {"bar": 1, ":path": "upserted"}, "masked": ["/input/:path"]}`,
			mask:  `{"op":"upsert","path":"/input/:path","value":"upserted"}`,
		},
		{
			note:  "all in one",
			event: `{"input": {"foo": {"bar": 12}}, "result": true, "nd_builtin_cache": {"rand.intn": {"[\"coin\", 2]": 1}}}`,
			mask: `[
				{ "op": "remove", "path": "/nd_builtin_cache/rand.intn"},
				"/result",
				{"op": "upsert", "path": "/input/foo/bar", "value": 1}
			]`,
			exp: `{"input": {"foo": {"bar": 1}}, "nd_builtin_cache": {}, "masked": ["/input/foo/bar"], "erased": ["/nd_builtin_cache/rand.intn", "/result"]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			var event, exp map[string]any
			if err := json.NewDecoder(strings.NewReader(tc.event)).Decode(&event); err != nil {
				t.Fatal(err)
			}
			if err := json.NewDecoder(strings.NewReader(tc.exp)).Decode(&exp); err != nil {
				t.Fatal(err)
			}

			var mask any
			if err := json.NewDecoder(strings.NewReader(tc.mask)).Decode(&mask); err != nil {
				t.Fatal(err)
			}

			var rules []any
			switch mask := mask.(type) {
			case []any:
				rules = mask
			default:
				rules = []any{mask}
			}
			act, err := maskEvent(rules, event)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(exp, act); diff != "" {
				t.Errorf("unexpected output (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMaskRuleErrors(t *testing.T) {
	// The mask policy is user-controlled. It may not alter any of the fields other
	// than `input`, `result` and `nd_builtin_cache`.
	tests := []struct {
		note, event, exp, mask string
	}{
		{
			note:  `erase req_id`,
			event: `{"input": {"a": 1}, "req_id": 12}`,
			mask:  `{"op":"remove","path":"/req_id"}`,
		},
		{
			note:  `erase req_id (plain)`,
			event: `{"input": {"a": 1}, "req_id": 12}`,
			mask:  `"/req_id"`,
		},
		{
			note:  `upsert req_id`,
			event: `{"input": {"a": 1}, "req_id": 12}`,
			mask:  `{"op":"remove","path":"/req_id","value": 13}`,
		},
		{
			note:  `upsert metrics/counter_regovm_eval_instructions (nested)`,
			event: `{"input": {"a": 1}, "req_id": 12}`,
			mask:  `{"op":"remove","path":"/metrics/counter_regovm_eval_instructions","value": 1}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			var event map[string]any
			if err := json.NewDecoder(strings.NewReader(tc.event)).Decode(&event); err != nil {
				t.Fatal(err)
			}

			var mask any
			if err := json.NewDecoder(strings.NewReader(tc.mask)).Decode(&mask); err != nil {
				t.Fatal(err)
			}

			var rules []any
			switch mask := mask.(type) {
			case []any:
				rules = mask
			default:
				rules = []any{mask}
			}
			act, err := maskEvent(rules, event)
			if err == nil {
				t.Fatalf("expected error, got result: %v", act)
			}
		})
	}
}
