package vm

import (
	"context"
	"math/big"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

func equalOp(ctx context.Context, a, b interface{}) (bool, error) {
	switch x := a.(type) {
	case fjson.Null:
		_, ok := b.(fjson.Null)
		return ok, nil

	case fjson.Bool:
		if y, ok := b.(fjson.Bool); ok {
			return x.Value() == y.Value(), nil
		}

		return false, nil

	case fjson.Float:
		if y, ok := b.(fjson.Float); ok {
			return compare(x, y) == 0, nil
		}

		return false, nil

	case *fjson.String:
		if y, ok := b.(*fjson.String); ok {
			return x.Value() == y.Value(), nil
		}

		return false, nil

	case fjson.Array:
		if y, ok := b.(fjson.Array); ok {
			if x.Len() != y.Len() {
				return false, nil
			}

			for i := 0; i < x.Len(); i++ {
				ok, err := equalOp(ctx, x.Iterate(i), y.Iterate(i))
				if !ok || err != nil {
					return false, err
				}
			}

			return true, nil
		}

		return false, nil

	case fjson.Object:
		return equalObject(ctx, a, b)

	case IterableObject:
		return equalObject(ctx, a, b)

	case *Set:
		if y, ok := b.(*Set); ok {
			return x.Equal(ctx, y)
		}

		return false, nil

	case hashable:
		if y, ok := b.(hashable); ok {
			return x.Equal(y), nil
		}

		return false, nil

	default:
		panic("unsupported type")
	}
}

func equalObject(ctx context.Context, a, b interface{}) (bool, error) {
	switch a := a.(type) {
	case fjson.Object:
		switch b := b.(type) {
		case fjson.Object:
			return a.Compare(b) == 0, nil

		case IterableObject:
			return equalObject(ctx, b, a)

		default:
			return false, nil
		}

	case IterableObject:
		switch b := b.(type) {
		case fjson.Object:
			var err error
			n := 0
			a.Iter(ctx, func(k, va T) bool {
				s, ok := k.(*fjson.String)
				if !ok {
					n = -1
					return true
				}

				vb := b.Value(s.Value())
				if vb == nil {
					n = -1
					return true
				}

				n++

				var eq bool
				eq, err = equalOp(ctx, va, vb)
				if err != nil {
					return true
				} else if !eq {
					n = -1
				}

				return !eq
			})
			if n < 0 {
				return false, nil
			}

			return b.Len() == n, err

		case IterableObject:
			var err error
			n := 0
			if err2 := a.Iter(ctx, func(k, va T) bool {
				var vb interface{}
				var ok bool
				vb, ok, err = b.Get(ctx, k)
				if err != nil {
					return true
				} else if !ok {
					n = -1
					return true
				}

				n++

				var eq bool
				eq, err = equalOp(ctx, va, vb)
				if err != nil {
					return true
				} else if !eq {
					n = -1
				}

				return !eq
			}); err2 != nil {
				return false, err2
			} else if err != nil {
				return false, err
			} else if n < 0 {
				return false, nil
			}

			m, err := b.Len(ctx)
			return n == m, err

		default:
			return false, nil
		}

	default:
		return false, nil
	}
}

func compare(x, y fjson.Float) int {
	a, b := x.Value(), y.Value()

	if ai, err := a.Int64(); err == nil {
		if bi, err := b.Int64(); err == nil {
			if ai == bi {
				return 0
			}
			if ai < bi {
				return -1
			}
			return 1
		}
	}

	// We use big.Rat for comparing big numbers.
	// It replaces big.Float due to following reason:
	// big.Float comes with a default precision of 64, and setting a
	// larger precision results in more memory being allocated
	// (regardless of the actual number we are parsing with SetString).
	//
	// Note: If we're so close to zero that big.Float says we are zero, do
	// *not* big.Rat).SetString on the original string it'll potentially
	// take very long.
	var bigA, bigB *big.Rat
	fa, ok := new(big.Float).SetString(string(a))
	if !ok {
		panic("illegal value")
	}
	if fa.IsInt() {
		if i, _ := fa.Int64(); i == 0 {
			bigA = new(big.Rat).SetInt64(0)
		}
	}
	if bigA == nil {
		bigA, ok = new(big.Rat).SetString(string(a))
		if !ok {
			panic("illegal value")
		}
	}

	fb, ok := new(big.Float).SetString(string(b))
	if !ok {
		panic("illegal value")
	}
	if fb.IsInt() {
		if i, _ := fb.Int64(); i == 0 {
			bigB = new(big.Rat).SetInt64(0)
		}
	}
	if bigB == nil {
		bigB, ok = new(big.Rat).SetString(string(b))
		if !ok {
			panic("illegal value")
		}
	}

	return bigA.Cmp(bigB)
}
