package json

import (
	"testing"
)

func TestPathParser(t *testing.T) {
	testPathParser(t, "$", true)
	testPathParser(t, "$.foo", true)
	testPathParser(t, "$.foo.bar", true)
	testPathParser(t, "$[0]", true)
	testPathParser(t, "$[0].foo", true)
	testPathParser(t, "$[0].foo[1]", true)
	testPathParser(t, "$[0].foo[1].bar", true)
	testPathParser(t, "$[*]", false)
	testPathParser(t, "$[*].bar", false)
	testPathParser(t, "$[*].bar[*]", false)
	testPathParser(t, "$['foo']", true)
	testPathParser(t, "$['foo']['bar']", true)
	testPathParser(t, "$['foo\\'bar']", true)
	testPathParser(t, "$['3 byte runes = 日本語']", true)
	testPathParser(t, "$['4 byte rune = \U0002101D']", true)
	testPathParser(t, "$['path/with/slash']", true)
	testPathParser(t, "$['path/with/slash']['foo']", true)
	testPathParser(t, "$['path/with/slash'].foo", true)

	testPathParserSpaces(t, " $ ", "$")
	testPathParserSpaces(t, "$[ 'foo' ].bar[ 0 ]", "$['foo'].bar[0]")
	testPathParserSpaces(t, "$[ ' foo ' ]", "$[' foo ']")
	testPathParserSpaces(t, "$[ * ].bar", "$[*].bar")
	testPathParserSpaces(t, "$[ 'foo\\'bar' ]", "$['foo\\'bar']")

	testPathParserFail(t, "")
	testPathParserFail(t, "$.")
	testPathParserFail(t, "$..")
	testPathParserFail(t, "$..*..*")
	testPathParserFail(t, "$[-1]")
	testPathParserFail(t, "$[3.14]")
	testPathParserFail(t, "$['foo]")
	testPathParserFail(t, "$['")
	testPathParserFail(t, "$[' ")
	testPathParserFail(t, "$[' '")
	testPathParserFail(t, "$[path/with/slash]")
	testPathParserFail(t, "$[path-no-slash]")
}

func TestPathTraverse(t *testing.T) {
	testPathTraverse(t, "$", "foo", []interface{}{"foo"}, true)
	testPathTraverse(t, "$[0]", []interface{}{"foo", "bar"}, []interface{}{"foo"}, true)
	testPathTraverse(t, "$.a.w",
		map[string]interface{}{
			"a": map[string]interface{}{"w": "1", "x": "2"},
		},
		[]interface{}{"1"},
		true)
	testPathTraverse(t, "$.a.w[0]['z']",
		map[string]interface{}{
			"a": map[string]interface{}{"w": []interface{}{map[string]interface{}{"z": "1"}, "2"}, "x": "3"},
		},
		[]interface{}{"1"},
		true)
	testPathTraverse(t, "$.a.w[0].z",
		map[string]interface{}{
			"a": map[string]interface{}{"w": []interface{}{map[string]interface{}{"z": "1"}, "2"}, "x": "3"},
		},
		[]interface{}{"1"},
		true)
	testPathTraverse(t, "$..*", []interface{}{"foo", "bar"}, []interface{}{"foo", "bar"}, false)
	testPathTraverse(t, "$..*", map[string]interface{}{"a": "1", "b": "2"}, []interface{}{"1", "2"}, false)
	testPathTraverse(t, "$..*",
		map[string]interface{}{
			"a": map[string]interface{}{"w": "1", "x": "2"},
			"b": map[string]interface{}{"y": "3", "z": "4"},
		},
		[]interface{}{
			map[string]interface{}{"w": "1", "x": "2"},
			map[string]interface{}{"y": "3", "z": "4"},
			"1", "2",
			"3", "4",
		},
		false)
	testPathTraverse(t, "$..[*]", []interface{}{"foo", "bar"}, []interface{}{"foo", "bar"}, false)
	testPathTraverse(t, "$..[0]", []interface{}{"foo", "bar"}, []interface{}{"foo"}, false)
	testPathTraverse(t, "$..[2]", []interface{}{"foo", "bar"}, []interface{}{}, false)
	testPathTraverse(t, "$['path/with/slash']",
		map[string]interface{}{
			"path/with/slash": "baz",
		},
		[]interface{}{"baz"},
		true)
	testPathTraverse(t, "$['path/with/slash']['foo']",
		map[string]interface{}{
			"path/with/slash": map[string]interface{}{"foo": "1"},
		},
		[]interface{}{"1"},
		true)
	testPathTraverse(t, "$['path/with/slash'].foo",
		map[string]interface{}{
			"path/with/slash": map[string]interface{}{"foo": "1"},
		},
		[]interface{}{"1"},
		true)
	testPathTraverse(t, "$['path-no-slash']",
		map[string]interface{}{
			"path-no-slash": "baz",
		},
		[]interface{}{"baz"},
		true)
	testPathTraverse(t, "$['path-no-slash']['foo']",
		map[string]interface{}{
			"path-no-slash": map[string]interface{}{"foo": "1"},
		},
		[]interface{}{"1"},
		true)
	testPathTraverse(t, "$['path-no-slash'].foo",
		map[string]interface{}{
			"path-no-slash": map[string]interface{}{"foo": "1"},
		},
		[]interface{}{"1"},
		true)
}

func testPathTraverse(t *testing.T, path string, doc interface{}, values []interface{}, singular bool) {
	jpath := testPathParser(t, path, singular)

	jdoc, err := buildJSON(doc)
	if err != nil {
		t.Fatalf("Unable to convert the value to internal JSON: %v", err)
	}

	correct := make([]Json, 0, len(values))
	for _, value := range values {
		jdoc, err := buildJSON(value)
		if err != nil {
			t.Fatalf("Unable to convert the value to internal JSON: %v", err)
		}
		correct = append(correct, jdoc)
	}

	var results []Json
	jdoc.Find(jpath, func(value Json) {
		results = append(results, value)
	})

	if len(results) != len(correct) {
		t.Errorf("Incorrect values length: %s vs %s", results, correct)
		return
	}

	for i := range results {
		if results[i].Compare(correct[i]) != 0 {
			t.Errorf("Incorrect values: %s vs %s", results, correct)
		}
	}
}

func testPathParser(t *testing.T, path string, singular bool) Path {
	j, err := ParsePath(path)
	if err != nil {
		t.Errorf("JSON path parsing failure: %v", err)
	}

	if j.String() != path {
		t.Errorf("Parsed JSON path does not result in identical string: %s vs %s", j.String(), path)
	}

	if singular != j.Singular() {
		t.Errorf("JSON path singularity failure: %s", path)
	}

	return j
}

func testPathParserSpaces(t *testing.T, path1 string, path2 string) {
	j1, err := ParsePath(path1)
	if err != nil {
		t.Errorf("JSON path parsing failure: %v", err)
	}

	if j1.String() != path2 {
		t.Errorf("Parsed JSON path does not result in identical string: %s vs %s", j1.String(), path2)
	}
}

func testPathParserFail(t *testing.T, path string) {
	_, err := ParsePath(path)
	if err == nil {
		t.Errorf("Illegal JSON path parsed")
	}
}
