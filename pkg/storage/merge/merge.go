// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package merge contains helpers to merge data structures
// frequently encountered in OPA.
package merge

import (
	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

// InterfaceMaps returns the result of merging a and b. If a and b cannot be
// merged because of conflicting key-value pairs, ok is false.
func InterfaceMaps(a, b bjson.Object) (bjson.Object, bool) {

	if a == nil {
		return b, true
	}

	if hasConflicts(a, b) {
		return nil, false
	}

	return merge(a, b), true
}

func merge(a, b bjson.Object) bjson.Object {

	ac := make(map[string]bjson.File, a.Len())

	for i, k := range a.Names() {
		ac[k] = a.Iterate(i)
	}

	for i, k := range b.Names() {
		add := b.Iterate(i)
		exist := ac[k]
		if exist == nil {
			ac[k] = add
			continue
		}

		existObj := exist.(bjson.Object)
		addObj := add.(bjson.Object)

		ac[k] = merge(existObj, addObj)
	}

	return bjson.NewObject(ac)
}

func hasConflicts(a, b bjson.Object) bool {
	for _, k := range b.Names() {

		add := b.Value(k)
		exist := a.Value(k)
		if exist == nil {
			continue
		}

		existObj, existOk := exist.(bjson.Object)
		addObj, addOk := add.(bjson.Object)
		if !existOk || !addOk {
			return true
		}

		if hasConflicts(existObj, addObj) {
			return true
		}
	}
	return false
}
