// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package vm

import (
	"context"
	"fmt"
	gostrings "strings"
)

// T is a concise way to refer to T.
type T = interface{}

type hashEntry struct {
	k    T
	v    T
	next *hashEntry
}

// HashMap represents a key/value map.
type HashMap struct {
	table map[int]*hashEntry
	size  int
}

// NewHashMap returns a new empty HashMap.
func NewHashMap() *HashMap {
	return &HashMap{
		table: nil,
		size:  0,
	}
}

// Copy returns a shallow copy of this HashMap.
func (h *HashMap) Copy(ctx context.Context) (*HashMap, error) {
	cpy := &HashMap{
		table: make(map[int]*hashEntry, len(h.table)),
		size:  0,
	}
	_, err := h.Iter(func(k, v T) (bool, error) {
		err := cpy.Put(ctx, k, v)
		return err != nil, err
	})
	return cpy, err
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

	for entry := h.table[hash]; entry != nil; entry = entry.next {
		eq, err := h.eq(ctx, entry.k, k)
		if err != nil {
			return nil, false, err
		}
		if eq {
			return entry.v, true, nil
		}
	}
	return nil, false, nil
}

// Delete removes the the key k.
func (h *HashMap) Delete(ctx context.Context, k T) error {
	if h.table == nil {
		return nil
	}

	hash, err := h.hash(ctx, k)
	if err != nil {
		return err
	}
	var prev *hashEntry
	for entry := h.table[hash]; entry != nil; entry = entry.next {
		eq, err := h.eq(ctx, entry.k, k)
		if err != nil {
			return err
		} else if eq {
			if prev != nil {
				prev.next = entry.next
			} else {
				h.table[hash] = entry.next
			}
			h.size--
			return nil
		}
		prev = entry
	}

	return nil
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
	if h.table == nil {
		return false, nil
	}

	for _, entry := range h.table {
		for ; entry != nil; entry = entry.next {
			if stop, err := iter(entry.k, entry.v); err != nil {
				return false, err
			} else if stop {
				return true, nil
			}
		}
	}
	return false, nil
}

// Len returns the current size of this HashMap.
func (h *HashMap) Len() int {
	return h.size
}

// Put inserts a key/value pair into this HashMap. If the key is already present, the existing
// value is overwritten.
func (h *HashMap) Put(ctx context.Context, k T, v T) error {
	hash, err := h.hash(ctx, k)
	if err != nil {
		return err
	}

	if h.table == nil {
		h.table = make(map[int]*hashEntry)
	}

	head := h.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		eq, err := h.eq(ctx, entry.k, k)
		if err != nil {
			return err
		}
		if eq {
			entry.v = v
			return nil
		}
	}
	h.table[hash] = &hashEntry{k: k, v: v, next: head}
	h.size++
	return nil
}

func (h *HashMap) String() string {
	var b gostrings.Builder
	b.WriteRune('{')
	i := 0
	h.Iter(func(k T, v T) (bool, error) {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString(fmt.Sprintf("%v: %v", k, v))
		return false, nil
	})
	b.WriteRune('}')
	return b.String()
}

// Update returns a new HashMap with elements from the other HashMap put into this HashMap.
// If the other HashMap contains elements with the same key as this HashMap, the value
// from the other HashMap overwrites the value from this HashMap.
func (h *HashMap) Update(ctx context.Context, other *HashMap) (*HashMap, error) {
	updated, err := h.Copy(ctx)
	if err != nil {
		return nil, err
	}
	_, err = other.Iter(func(k, v T) (bool, error) {
		err := updated.Put(ctx, k, v)
		return err != nil, err
	})
	return updated, err
}

func (h *HashMap) hash(ctx context.Context, v T) (int, error) {
	x, err := hash(ctx, v)
	return int(x), err
}

func (h *HashMap) eq(ctx context.Context, a, b T) (bool, error) {
	return equalOp(ctx, a, b)
}
