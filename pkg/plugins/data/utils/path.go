// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage"
)

func AddDataPrefixAndParsePath(path string) (storage.Path, error) {
	ref, err := ast.ParseRef("data." + path)
	if err != nil {
		return nil, err
	}
	return storage.NewPathForRef(ref)
}
