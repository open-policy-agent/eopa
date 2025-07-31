// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

func UnionObjects(a, b Json) Json {
	switch a := a.(type) {
	case Object:
		return a.Union(b)

	case Object2:
		switch b := b.(type) {
		case Object:
			return b.Union(a)
		case Object2:
			return a.Union(b)
		}
	}

	return b
}
