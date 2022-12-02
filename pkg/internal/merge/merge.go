// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package merge contains helpers to merge data structures
// frequently encountered in OPA.
package merge

import bjson "github.com/styrainc/load/pkg/json"

// InterfaceMaps returns the result of merging a and b. If a and b cannot be
// merged because of conflicting key-value pairs, ok is false.
func InterfaceMaps(a bjson.Object, b bjson.Object) (bjson.Object, bool) {

	if a == nil {
		return b, true
	}

	if hasConflicts(a, b) {
		return nil, false
	}

	return merge(a, b), true
}

func merge(a, b bjson.Object) bjson.Object {
	r := a.Clone(false).(bjson.Object)

	for _, k := range b.Names() {
		add := b.Value(k)
		exist := a.Value(k)
		if exist == nil {
			r.Set(k, add)
			continue
		}

		r.Set(k, merge(exist.(bjson.Object), add.(bjson.Object)))
	}

	return r
}

func hasConflicts(a, b bjson.Object) bool {
	for _, k := range b.Names() {
		add := b.Value(k)

		exist := a.Value(k)
		if exist == nil {
			continue
		}

		_, existOk := exist.(bjson.Object)
		_, addOk := add.(bjson.Object)

		if !existOk || !addOk {
			return true
		}

		if hasConflicts(exist.(bjson.Object), add.(bjson.Object)) {
			return true
		}
	}

	return false
}
