package vm

import (
	"context"
)

type hashSetEntry struct {
	k    T
	next *hashSetEntry
}

// HashSet represents a key/value map.
type HashSet struct {
	table map[int]*hashSetEntry
	size  int
}

// NewHashSet returns a new empty HashSet.
func NewHashSet() *HashSet {
	return &HashSet{
		table: nil,
		size:  0,
	}
}

// Equal returns true if this HashSet equals the other HashSet.
// Two hash maps are equal if they contain the same values.
func (h *HashSet) Equal(ctx context.Context, other *HashSet) (bool, error) {
	if h.Len() != other.Len() {
		return false, nil
	}
	neq, err := h.Iter(func(k T) (bool, error) {
		eq, err := other.Get(ctx, k)
		if err != nil {
			return true, err
		}
		return !eq, err
	})
	return !neq, err
}

// Get checks if the value is in the set.
func (h *HashSet) Get(ctx context.Context, k T) (bool, error) {
	if h.table == nil {
		return false, nil
	}

	hash, err := h.hash(ctx, k)
	if err != nil {
		return false, err
	}
	for entry := h.table[hash]; entry != nil; entry = entry.next {
		eq, err := h.eq(ctx, entry.k, k)
		if err != nil {
			return false, err
		} else if eq {
			return true, nil
		}
	}

	return false, nil
}

// Hash returns the hash code for this hash map.
func (h *HashSet) Hash(ctx context.Context) (int, error) {
	var hash int
	_, err := h.Iter(func(k T) (bool, error) {
		v, err := h.hash(ctx, k)
		if err != nil {
			return true, err
		}
		hash += v
		return false, nil
	})
	return hash, err
}

// Iter invokes the iter function for each element in the HashSet.
// If the iter function returns true, iteration stops and the return value is true.
// If the iter function never returns true, iteration proceeds through all elements
// and the return value is false.
func (h *HashSet) Iter(iter func(T) (bool, error)) (bool, error) {
	if h.table == nil {
		return false, nil
	}

	for _, entry := range h.table {
		for ; entry != nil; entry = entry.next {
			if stop, err := iter(entry.k); err != nil {
				return false, err
			} else if stop {
				return true, nil
			}
		}
	}
	return false, nil
}

// Len returns the current size of this HashSet.
func (h *HashSet) Len() int {
	return h.size
}

// Put inserts a value into this HashSet. If the value is already
// present, the operation is a no op.
func (h *HashSet) Put(ctx context.Context, k T) error {
	hash, err := h.hash(ctx, k)
	if err != nil {
		return err
	}

	if h.table == nil {
		h.table = make(map[int]*hashSetEntry)
	}

	head := h.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		eq, err := h.eq(ctx, entry.k, k)
		if err != nil {
			return err
		}
		if eq {
			return nil
		}
	}
	h.table[hash] = &hashSetEntry{k: k, next: head}
	h.size++
	return nil
}

func (h *HashSet) hash(ctx context.Context, v T) (int, error) {
	x, err := hash(ctx, v)
	return int(x), err
}

func (h *HashSet) eq(ctx context.Context, a, b T) (bool, error) {
	return equalOp(ctx, a, b)
}
