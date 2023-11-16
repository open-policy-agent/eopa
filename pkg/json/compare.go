package json

import (
	"errors"
	"math/big"
)

var errNotEqual = errors.New("not equal")

func Equal(a, b Json) (bool, error) {
	return equalOp(a, b)
}

func equalOp(a, b Json) (bool, error) {
	switch x := a.(type) {
	case Null:
		_, ok := b.(Null)
		return ok, nil

	case Bool:
		if y, ok := b.(Bool); ok {
			return x.Value() == y.Value(), nil
		}

		return false, nil

	case Float:
		if y, ok := b.(Float); ok {
			return compareFloat(x, y) == 0, nil
		}

		return false, nil

	case *String:
		if y, ok := b.(*String); ok {
			return x.Value() == y.Value(), nil
		}

		return false, nil

	case Array:
		if y, ok := b.(Array); ok {
			if x.Len() != y.Len() {
				return false, nil
			}

			for i := 0; i < x.Len(); i++ {
				ok, err := equalOp(x.Iterate(i), y.Iterate(i))
				if !ok || err != nil {
					return false, err
				}
			}

			return true, nil
		}

		return false, nil

	case Object:
		return equalObject(a, b)

	case Object2:
		return equalObject(a, b)

	case Set:
		if y, ok := b.(Set); ok {
			return x.Equal(y)
		}

		return false, nil

	default:
		panic("unsupported type")
	}
}

func equalObject(a, b interface{}) (bool, error) {
	switch a := a.(type) {
	case Object:
		switch b := b.(type) {
		case Object:
			return a.Compare(b) == 0, nil

		case Object2:
			if a.Len() != b.Len() {
				return false, nil
			}

			if err := b.Iter(func(k, vb Json) (bool, error) {
				s, ok := k.(*String)
				if !ok {
					return true, errNotEqual
				}

				va := a.Value(s.Value())
				if va == nil {
					return true, errNotEqual
				}

				if eq, err := equalOp(va, vb); err != nil {
					return true, err
				} else if !eq {
					return true, errNotEqual
				}

				return false, nil
			}); errors.Is(err, errNotEqual) {
				return false, nil
			} else if err != nil {
				return false, err
			}

			return true, nil
		}

	case Object2:
		switch b := b.(type) {
		case Object:
			return equalOp(b, a)

		case Object2:
			return a.Equal(b)
		}
	}

	return false, nil
}

func compareFloat(x, y Float) int {
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
