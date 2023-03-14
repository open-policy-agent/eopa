package vm

import (
	"fmt"
	gostrings "strings"
)

type hashSetEntry struct {
	k    T
	next *hashSetEntry
}

// HashSet represents a key/value map.
type HashSet struct {
	eq    func(T, T) bool
	hash  func(T) int
	table map[int]*hashSetEntry
	size  int
}

// NewHashSet returns a new empty HashSet.
func NewHashSet(eq func(T, T) bool, hash func(T) int) *HashSet {
	return &HashSet{
		eq:    eq,
		hash:  hash,
		table: make(map[int]*hashSetEntry),
		size:  0,
	}
}

// Copy returns a shallow copy of this HashSet.
func (h *HashSet) Copy() *HashSet {
	cpy := &HashSet{
		eq:    h.eq,
		hash:  h.hash,
		table: make(map[int]*hashSetEntry, len(h.table)),
		size:  0,
	}
	h.Iter(func(k T) bool {
		cpy.Put(k)
		return false
	})
	return cpy
}

// Equal returns true if this HashSet equals the other HashSet.
// Two hash maps are equal if they contain the same values.
func (h *HashSet) Equal(other *HashSet) bool {
	if h.Len() != other.Len() {
		return false
	}
	return !h.Iter(func(k T) bool {
		return !other.Get(k)
	})
}

// Get checks if the value is in the set.
func (h *HashSet) Get(k T) bool {
	hash := h.hash(k)
	for entry := h.table[hash]; entry != nil; entry = entry.next {
		if h.eq(entry.k, k) {
			return true
		}
	}

	return false
}

// Delete removes the the key k.
func (h *HashSet) Delete(k T) {
	hash := h.hash(k)
	var prev *hashSetEntry
	for entry := h.table[hash]; entry != nil; entry = entry.next {
		if h.eq(entry.k, k) {
			if prev != nil {
				prev.next = entry.next
			} else {
				h.table[hash] = entry.next
			}
			h.size--
			return
		}
		prev = entry
	}
}

// Hash returns the hash code for this hash map.
func (h *HashSet) Hash() int {
	var hash int
	h.Iter(func(k T) bool {
		hash += h.hash(k)
		return false
	})
	return hash
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
func (h *HashSet) Put(k T) {
	hash := h.hash(k)
	head := h.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		if h.eq(entry.k, k) {
			return
		}
	}
	h.table[hash] = &hashSetEntry{k: k, next: head}
	h.size++
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
func (h *HashSet) Update(other *HashSet) *HashSet {
	updated := h.Copy()
	other.Iter(func(k T) bool {
		updated.Put(k)
		return false
	})
	return updated
}
