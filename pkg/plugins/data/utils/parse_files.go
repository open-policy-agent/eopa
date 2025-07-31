// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/open-policy-agent/opa/v1/util"
)

type (
	Parser func(io.Reader) (any, error)
)

// ParseJSONOrYaml decodes a given json or yaml stream to go representation
func ParseJSONOrYaml(r io.Reader) (any, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var v any
	err = util.Unmarshal(data, &v)
	return v, err
}

// ParseFile decodes json, yaml, yml or xml files
func ParseFile(filename string, r io.Reader) (any, error) {
	var parser Parser
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".json", ".yaml", ".yml":
		parser = ParseJSONOrYaml
	case ".xml":
		parser = ParseXML
	default:
		// ignore if not json or yaml
		return nil, nil
	}
	if cr, ok := r.(io.Closer); ok {
		defer cr.Close()
	}
	data, err := parser(r)
	if err != nil {
		return nil, fmt.Errorf("parsing file %q: %w", filename, err)
	}
	return data, nil
}

// InsertFile inserts given document to the files map by given path
func InsertFile(files map[string]any, path []string, document any) {
	if len(path) == 1 {
		files[path[0]] = document
		return
	}

	child, ok := files[path[0]]
	if !ok {
		child = make(map[string]any)
		files[path[0]] = child
	}

	InsertFile(child.(map[string]any), path[1:], document)
}
