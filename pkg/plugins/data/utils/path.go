package utils

import (
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"
)

func AddDataPrefixAndParsePath(path string) (storage.Path, error) {
	ref, err := ast.ParseRef("data." + path)
	if err != nil {
		return nil, err
	}
	return storage.NewPathForRef(ref)
}
