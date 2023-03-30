package vm

import (
	"context"
	"fmt"
	gostrings "strings"
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
		table: make(map[int]*hashSetEntry),
		size:  0,
	}
}

// Copy returns a shallow copy of this HashSet.
func (h *HashSet) Copy(ctx context.Context) (*HashSet, error) {
	cpy := &HashSet{
		table: make(map[int]*hashSetEntry, len(h.table)),
		size:  0,
	}
	var err error
	h.Iter(func(k T) bool {
		if err = cpy.Put(ctx, k); err != nil {
			return true
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	return cpy, nil
}

// Equal returns true if this HashSet equals the other HashSet.
// Two hash maps are equal if they contain the same values.
func (h *HashSet) Equal(ctx context.Context, other *HashSet) (bool, error) {
	if h.Len() != other.Len() {
		return false, nil
	}
	var err error
	neq := h.Iter(func(k T) bool {
		var eq bool
		eq, err = other.Get(ctx, k)
		if err != nil {
			return true
		}
		return !eq
	})
	return !neq, err
}

// Get checks if the value is in the set.
func (h *HashSet) Get(ctx context.Context, k T) (bool, error) {
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

// Delete removes the the key k.
func (h *HashSet) Delete(ctx context.Context, k T) error {
	hash, err := h.hash(ctx, k)
	if err != nil {
		return err
	}
	var prev *hashSetEntry
	for entry := h.table[hash]; entry != nil; entry = entry.next {
		eq, err := h.eq(ctx, entry.k, k)
		if err != nil {
			return err
		}
		if eq {
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
func (h *HashSet) Hash(ctx context.Context) (int, error) {
	var hash int
	var err error
	h.Iter(func(k T) bool {
		var v int
		v, err = h.hash(ctx, k)
		if err != nil {
			return true
		}
		hash += v
		return false
	})
	return hash, err
}

// Iter invokes the iter function for each element in the HashSet.
// If the iter function returns true, iteration stops and the return value is true.
// If the iter function never returns true, iteration proceeds through all elements
// and the return value is false.
func (h *HashSet) Iter(iter func(T) bool) bool {
	for _, entry := range h.table {
		for ; entry != nil; entry = entry.next {
			if iter(entry.k) {
				return true
			}
		}
	}
	return false
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

func (h *HashSet) String() string {
	var b gostrings.Builder
	b.WriteRune('{')
	i := 0
	h.Iter(func(k T) bool {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%v", k))
		i++
		return false
	})
	b.WriteRune('}')
	return b.String()
}

// Update returns a new HashSet with elements from the other HashSet put into this HashSet.
func (h *HashSet) Update(ctx context.Context, other *HashSet) (*HashSet, error) {
	updated, err := h.Copy(ctx)
	if err != nil {
		return nil, err
	}
	other.Iter(func(k T) bool {
		err = updated.Put(ctx, k)
		return err != nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (h *HashSet) hash(ctx context.Context, v T) (int, error) {
	x, err := hash(ctx, v)
	return int(x), err
}

func (h *HashSet) eq(ctx context.Context, a, b T) (bool, error) {
	return equalOp(ctx, a, b)
}
