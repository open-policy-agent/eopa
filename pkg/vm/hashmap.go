// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package vm

import (
	"context"
)

// T is a concise way to refer to T.
type T = interface{}

type hashEntry struct {
	hash int
	k    T
	v    T
}

// HashMap represents a key/value map.
type HashMap struct {
	table []hashEntry
}

// NewHashMap returns a new empty HashMap.
func NewHashMap() *HashMap {
	return &HashMap{
		table: nil,
	}
}

// Equal returns true if this HashMap equals the other HashMap.
// Two hash maps are equal if they contain the same key/value pairs.
func (h *HashMap) Equal(ctx context.Context, other *HashMap) (bool, error) {
	if h.Len() != other.Len() {
		return false, nil
	}
	stop, err := h.Iter(func(k, v T) (bool, error) {
		ov, ok, err := other.Get(ctx, k)
		if err != nil {
			return true, err
		} else if !ok {
			return true, nil
		}

		eq, err := h.eq(ctx, v, ov)
		if err != nil {
			return true, err
		}

		return !eq, nil
	})
	return !stop, err
}

// Get returns the value for k.
func (h *HashMap) Get(ctx context.Context, k T) (T, bool, error) {
	if h.table == nil {
		return nil, false, nil
	}

	hash, err := h.hash(ctx, k)
	if err != nil {
		return nil, false, err
	}

	for i := 0; i < len(h.table); i++ {
		if h.table[i].hash == hash {
			eq, err := h.eq(ctx, h.table[i].k, k)
			if err != nil {
				return nil, false, err
			}
			if eq {
				return h.table[i].v, true, nil
			}
		}
	}
	return nil, false, nil
}

// Hash returns the hash code for this hash map.
func (h *HashMap) Hash(ctx context.Context) (int, error) {
	var hash int
	_, err := h.Iter(func(k, v T) (bool, error) {
		kh, err := h.hash(ctx, k)
		if err != nil {
			return true, err
		}

		vh, err := h.hash(ctx, v)
		if err != nil {
			return true, err
		}

		hash += kh + vh
		return false, nil
	})
	return hash, err
}

// Iter invokes the iter function for each element in the HashMap.
// If the iter function returns true, iteration stops and the return value is true.
// If the iter function never returns true, iteration proceeds through all elements
// and the return value is false.
func (h *HashMap) Iter(iter func(T, T) (bool, error)) (bool, error) {
	for i := 0; i < len(h.table); i++ {
		if stop, err := iter(h.table[i].k, h.table[i].v); err != nil {
			return false, err
		} else if stop {
			return true, nil
		}
	}
	return false, nil
}

// Len returns the current size of this HashMap.
func (h *HashMap) Len() int {
	return len(h.table)
}

// Put inserts a key/value pair into this HashMap. If the key is already present, the existing
// value is overwritten.
func (h *HashMap) Put(ctx context.Context, k T, v T) error {
	hash, err := h.hash(ctx, k)
	if err != nil {
		return err
	}

	for i := 0; i < len(h.table); i++ {
		if h.table[i].hash == hash {
			eq, err := h.eq(ctx, h.table[i].k, k)
			if err != nil {
				return err
			}
			if eq {
				h.table[i].v = v
				return nil
			}
		}
	}

	h.table = append(h.table, hashEntry{hash: hash, k: k, v: v})
	return nil
}

func (h *HashMap) hash(ctx context.Context, v T) (int, error) {
	x, err := hash(ctx, v)
	return int(x), err
}

func (h *HashMap) eq(ctx context.Context, a, b T) (bool, error) {
	return equalOp(ctx, a, b)
}
