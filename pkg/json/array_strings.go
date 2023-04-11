package json

import (
	"io"

	"github.com/open-policy-agent/opa/ast"
)

// ArraySliceCompactStrings is a compact implementation of the string
// arrays with at most 32 elements.
type ArraySliceCompactStrings[T indexableStrings] struct {
	elements T
}

func NewArrayCompactStrings(elements []File) Array {
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

	switch len(elements) {
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
		return NewArray2(elements)
	}
}

func newArrayCompactStrings[T indexableStrings](elements []File) Array {
	var a ArraySliceCompactStrings[T]
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
	return a.clone(false).Append(elements...)
}

func (a *ArraySliceCompactStrings[T]) AppendSingle(element File) (Array, bool) {
	n, _ := a.clone(false).AppendSingle(element)
	return n, true
}

func (a *ArraySliceCompactStrings[T]) Slice(i, j int) Array {
	elements := make([]File, j-i)
	for k := 0; k < len(elements); k++ {
		elements[k] = a.elements[i+k]
	}

	return NewArray2(elements)
}

func (a *ArraySliceCompactStrings[T]) Len() int {
	return len(a.elements)
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
	return a.clone(false).RemoveIdx(i)
}

func (a *ArraySliceCompactStrings[T]) SetIdx(i int, j File) Json {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	if s, ok := j.(*String); ok {
		a.elements[i] = s
	}

	return a.clone(false).SetIdx(i, j)
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

func (a *ArraySliceCompactStrings[T]) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, a, finder)
}

func (a *ArraySliceCompactStrings[T]) Compare(other Json) int {
	return compare(a, other)
}

func (a *ArraySliceCompactStrings[T]) Walk(state *DecodingState, walker Walker) {
	arrayWalk(a, state, walker)
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
