// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package sql

import (
	"github.com/open-policy-agent/opa/v1/storage"
)

type (
	tableTrie struct {
		nodes map[string]*tableTrie
		table *TableOpt
	}
)

const pathWildcard = "*"

func buildTablesTrie(tables []TableOpt) *tableTrie {
	root := newTableTrie()
	for _, table := range tables {
		if !root.insert(table.Path, table) {
			return nil
		}
	}
	return root
}

func newTableTrie() *tableTrie {
	return &tableTrie{
		nodes: make(map[string]*tableTrie),
	}
}

func (t *tableTrie) Find(path storage.Path) []TableOpt {
	node := t
	for i := 0; i < len(path) && node.table == nil; i++ {
		next, ok := node.nodes[pathWildcard]
		if ok {
			node = next
			continue
		}

		next, ok = node.nodes[path[i]]
		if !ok {
			return nil
		}

		node = next
	}

	return node.tables()
}

func (t *tableTrie) insert(path storage.Path, table TableOpt) bool {
	if len(path) == 0 {
		if len(t.nodes) != 0 {
			return false
		}

		t.table = &table
		return true
	}

	head := path[0]
	child, ok := t.nodes[head]
	if !ok {
		if t.table != nil {
			return false
		}

		child = newTableTrie()
		t.nodes[head] = child
	}

	return child.insert(path[1:], table)
}

func (t *tableTrie) tables() []TableOpt {
	if t.table != nil {
		return []TableOpt{*t.table}
	}

	var tables []TableOpt
	for _, node := range t.nodes {
		tables = append(tables, node.tables()...)
	}

	return tables
}
