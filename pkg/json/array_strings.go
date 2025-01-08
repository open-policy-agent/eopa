package json

import (
	"io"

	"github.com/open-policy-agent/opa/v1/ast"
)

// ArraySliceCompactStrings is a compact implementation of the string
// arrays with at most 32 elements.
type ArraySliceCompactStrings[T indexableStrings] struct {
	n        int
	elements T
}

func NewArrayCompactStrings(elements []File, cap int) Array {
	n := len(elements)
	if n == 0 || n > 32 {
		return nil
	}

	for _, f := range elements {
		switch f.(type) {
		case *String:
		default:
			return nil
		}
	}

	switch cap {
	case 1:
		return newArrayCompactStrings[[1]*String](elements)
	case 2:
		return newArrayCompactStrings[[2]*String](elements)
	case 3:
		return newArrayCompactStrings[[3]*String](elements)
	case 4:
		return newArrayCompactStrings[[4]*String](elements)
	case 5:
		return newArrayCompactStrings[[5]*String](elements)
	case 6:
		return newArrayCompactStrings[[6]*String](elements)
	case 7:
		return newArrayCompactStrings[[7]*String](elements)
	case 8:
		return newArrayCompactStrings[[8]*String](elements)
	case 9:
		return newArrayCompactStrings[[9]*String](elements)
	case 10:
		return newArrayCompactStrings[[10]*String](elements)
	case 11:
		return newArrayCompactStrings[[11]*String](elements)
	case 12:
		return newArrayCompactStrings[[12]*String](elements)
	case 13:
		return newArrayCompactStrings[[13]*String](elements)
	case 14:
		return newArrayCompactStrings[[14]*String](elements)
	case 15:
		return newArrayCompactStrings[[15]*String](elements)
	case 16:
		return newArrayCompactStrings[[16]*String](elements)
	case 17:
		return newArrayCompactStrings[[17]*String](elements)
	case 18:
		return newArrayCompactStrings[[18]*String](elements)
	case 19:
		return newArrayCompactStrings[[19]*String](elements)
	case 20:
		return newArrayCompactStrings[[20]*String](elements)
	case 21:
		return newArrayCompactStrings[[21]*String](elements)
	case 22:
		return newArrayCompactStrings[[22]*String](elements)
	case 23:
		return newArrayCompactStrings[[23]*String](elements)
	case 24:
		return newArrayCompactStrings[[24]*String](elements)
	case 25:
		return newArrayCompactStrings[[25]*String](elements)
	case 26:
		return newArrayCompactStrings[[26]*String](elements)
	case 27:
		return newArrayCompactStrings[[27]*String](elements)
	case 28:
		return newArrayCompactStrings[[28]*String](elements)
	case 29:
		return newArrayCompactStrings[[29]*String](elements)
	case 30:
		return newArrayCompactStrings[[30]*String](elements)
	case 31:
		return newArrayCompactStrings[[31]*String](elements)
	case 32:
		return newArrayCompactStrings[[32]*String](elements)
	default:
		return &ArraySlice{elements}
	}
}

func newArrayCompactStrings[T indexableStrings](elements []File) Array {
	a := ArraySliceCompactStrings[T]{n: len(elements)}
	for i := range elements {
		a.elements[i] = elements[i].(*String)
	}

	return &a
}

func (a *ArraySliceCompactStrings[T]) WriteTo(w io.Writer) (int64, error) {
	return writeArrayJSON(w, a)
}

func (a *ArraySliceCompactStrings[T]) Contents() interface{} {
	return a.JSON()
}

func (a *ArraySliceCompactStrings[T]) Append(elements ...File) Array {
	if a.n+len(elements) <= len(a.elements) {
		for _, element := range elements {
			switch element.(type) {
			case *String:
			default:
				return a.append(elements...)
			}
		}

		for i, element := range elements {
			a.elements[a.n+i] = element.(*String)
		}

		a.n += len(elements)
		return a
	}

	return a.append(elements...)
}

func (a *ArraySliceCompactStrings[T]) AppendSingle(element File) (Array, bool) {
	if a.n+1 <= len(a.elements) {
		switch s := element.(type) {
		case *String:
			a.elements[a.n] = s
			a.n++
			return a, false
		}
	}

	return a.append(element), true
}

func (a *ArraySliceCompactStrings[T]) append(elements ...File) Array {
	m := a.n + len(elements)
	n := make([]File, m)
	j := 0
	for i := 0; i < a.n; i++ {
		n[j] = a.elements[i]
		j++
	}
	for i := 0; i < len(elements); i++ {
		n[j] = elements[i]
		j++
	}

	p := 1
	for p < m {
		p *= 2
	}

	return NewArray(n, p)
}

func (a *ArraySliceCompactStrings[T]) Slice(i, j int) Array {
	elements := make([]File, j-i)
	for k := 0; k < len(elements); k++ {
		elements[k] = a.elements[i+k]
	}

	return NewArray(elements, len(elements))
}

func (a *ArraySliceCompactStrings[T]) Len() int {
	return a.n
}

func (a *ArraySliceCompactStrings[T]) Value(i int) Json {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	return a.elements[i]
}

func (a *ArraySliceCompactStrings[T]) valueImpl(i int) File {
	return a.elements[i]
}

func (a *ArraySliceCompactStrings[T]) WriteI(w io.Writer, i int, written *int64) error {
	return arraySliceBase[*ArraySliceCompactStrings[T]]{}.WriteI(a, w, i, written)
}

func (a *ArraySliceCompactStrings[T]) Iterate(i int) Json {
	return a.Value(i)
}

func (a *ArraySliceCompactStrings[T]) iterate(i int) File {
	return a.valueImpl(i)
}

func (a *ArraySliceCompactStrings[T]) RemoveIdx(i int) Json {
	if a.n == 1 {
		return zeroArray
	}

	for ; i < a.n-1; i++ {
		a.elements[i] = a.elements[i+1]
	}

	a.n--
	a.elements[a.n] = nil
	return a
}

func (a *ArraySliceCompactStrings[T]) SetIdx(i int, j File) Json {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	if s, ok := j.(*String); ok {
		a.elements[i] = s
		return a
	}

	f := make([]File, a.Len())
	for k := 0; k < len(f); k++ {
		if k == i {
			f[k] = j
		} else {
			f[k] = a.valueImpl(k)
		}
	}

	return NewArray(f, len(f))
}

func (a *ArraySliceCompactStrings[T]) JSON() interface{} {
	return arraySliceBase[*ArraySliceCompactStrings[T]]{}.JSON(a)
}

func (a *ArraySliceCompactStrings[T]) AST() ast.Value {
	return arraySliceBase[*ArraySliceCompactStrings[T]]{}.AST(a)
}

func (a *ArraySliceCompactStrings[T]) Extract(ptr string) (Json, error) {
	return arraySliceBase[*ArraySliceCompactStrings[T]]{}.Extract(a, ptr)
}

func (a *ArraySliceCompactStrings[T]) extractImpl(ptr []string) (Json, error) {
	return arraySliceBase[*ArraySliceCompactStrings[T]]{}.extractImpl(a, ptr)
}

func (a *ArraySliceCompactStrings[T]) Compare(other Json) int {
	return compare(a, other)
}

func (a *ArraySliceCompactStrings[T]) Clone(deepCopy bool) File {
	return a.clone(deepCopy)
}

func (a *ArraySliceCompactStrings[T]) clone(deepCopy bool) Array {
	return arraySliceBase[*ArraySliceCompactStrings[T]]{}.clone(a, deepCopy)
}

func (a *ArraySliceCompactStrings[T]) String() string {
	return arraySliceBase[*ArraySliceCompactStrings[T]]{}.String(a)
}
