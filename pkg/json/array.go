package json

import (
	"io"
	"strings"

	"github.com/open-policy-agent/opa/ast"
)

const maxCompactArray = 32

var zeroArray = newArrayCompact[[0]File](nil)

type arraySliceBase[T Array] struct{}

func (arraySliceBase[T]) WriteI(a T, w io.Writer, i int, written *int64) error {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	n, err := a.valueImpl(i).WriteTo(w)
	*written += int64(n)
	return err
}

func (arraySliceBase[T]) Extract(a T, ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return a.extractImpl(p)
}

func (arraySliceBase[T]) extractImpl(a T, ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return a, nil
	}

	i, err := parseInt(ptr[0])
	if err != nil {
		return nil, errPathNotFound
	}

	if i < 0 || i >= a.Len() {
		return nil, errPathNotFound
	}

	return a.Value(i).extractImpl(ptr[1:])
}

func (arraySliceBase[T]) clone(a T, deepCopy bool) Array {
	j := make([]File, a.Len())
	for i := 0; i < len(j); i++ {
		v := a.valueImpl(i)
		if deepCopy {
			v = v.Clone(true)
		}
		j[i] = v
	}

	return NewArray(j, len(j))
}

func (arraySliceBase[T]) JSON(a T) interface{} {
	array := make([]interface{}, a.Len())
	for i := 0; i < len(array); i++ {
		j, ok := a.valueImpl(i).(Json)
		if ok {
			array[i] = j.JSON()
		}
	}

	return array
}

func (arraySliceBase[T]) AST(a T) ast.Value {
	array := make([]*ast.Term, a.Len())
	for i := 0; i < len(array); i++ {
		j, ok := a.valueImpl(i).(Json)
		if ok {
			array[i] = ast.NewTerm(j.AST())
		}
	}

	return ast.NewArray(array...)
}

func (arraySliceBase[T]) String(a T) string {
	s := make([]string, a.Len())
	for i := 0; i < len(s); i++ {
		s[i] = a.Value(i).String()
	}
	return "[" + strings.Join(s, ",") + "]"
}

// ArraySliceCompact is a compact implementation of the arrays with at
// most 32 elements.
type ArraySliceCompact[T indexable] struct {
	n        int
	elements T
}

func NewArray(elements []File, cap int) Array {
	if a := NewArrayCompactStrings(elements, cap); a != nil {
		return a
	}

	switch cap {
	case 0:
		return zeroArray
	case 1:
		return newArrayCompact[[1]File](elements)
	case 2:
		return newArrayCompact[[2]File](elements)
	case 3:
		return newArrayCompact[[3]File](elements)
	case 4:
		return newArrayCompact[[4]File](elements)
	case 5:
		return newArrayCompact[[5]File](elements)
	case 6:
		return newArrayCompact[[6]File](elements)
	case 7:
		return newArrayCompact[[7]File](elements)
	case 8:
		return newArrayCompact[[8]File](elements)
	case 9:
		return newArrayCompact[[9]File](elements)
	case 10:
		return newArrayCompact[[10]File](elements)
	case 11:
		return newArrayCompact[[11]File](elements)
	case 12:
		return newArrayCompact[[12]File](elements)
	case 13:
		return newArrayCompact[[13]File](elements)
	case 14:
		return newArrayCompact[[14]File](elements)
	case 15:
		return newArrayCompact[[15]File](elements)
	case 16:
		return newArrayCompact[[16]File](elements)
	case 17:
		return newArrayCompact[[17]File](elements)
	case 18:
		return newArrayCompact[[18]File](elements)
	case 19:
		return newArrayCompact[[19]File](elements)
	case 20:
		return newArrayCompact[[20]File](elements)
	case 21:
		return newArrayCompact[[21]File](elements)
	case 22:
		return newArrayCompact[[22]File](elements)
	case 23:
		return newArrayCompact[[23]File](elements)
	case 24:
		return newArrayCompact[[24]File](elements)
	case 25:
		return newArrayCompact[[25]File](elements)
	case 26:
		return newArrayCompact[[26]File](elements)
	case 27:
		return newArrayCompact[[27]File](elements)
	case 28:
		return newArrayCompact[[28]File](elements)
	case 29:
		return newArrayCompact[[29]File](elements)
	case 30:
		return newArrayCompact[[30]File](elements)
	case 31:
		return newArrayCompact[[31]File](elements)
	case 32:
		return newArrayCompact[[32]File](elements)
	default:
		return &ArraySlice{elements}
	}
}

func newArrayCompact[T indexable](elements []File) Array {
	a := ArraySliceCompact[T]{n: len(elements)}
	for i := range elements {
		a.elements[i] = elements[i]
	}
	return &a
}

func (a *ArraySliceCompact[T]) WriteTo(w io.Writer) (int64, error) {
	return writeArrayJSON(w, a)
}

func (a *ArraySliceCompact[T]) Contents() interface{} {
	return a.JSON()
}

func (a *ArraySliceCompact[T]) Append(elements ...File) Array {
	if a.n+len(elements) <= len(a.elements) {
		for i, element := range elements {
			a.elements[a.n+i] = element
		}

		a.n += len(elements)
		return a
	}

	return a.append(elements...)
}

func (a *ArraySliceCompact[T]) AppendSingle(element File) (Array, bool) {
	if a.n+1 <= len(a.elements) {
		a.elements[a.n] = element
		a.n++
		return a, false
	}

	return a.append(element), true
}

// append appends the elements to the array, doubling the capacity as
// many times as necessary.
func (a *ArraySliceCompact[T]) append(elements ...File) Array {
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

func (a *ArraySliceCompact[T]) Slice(i, j int) Array {
	elements := make([]File, j-i)
	for k := 0; k < len(elements); k++ {
		elements[k] = a.elements[i+k]
	}

	return NewArray(elements, len(elements))
}

func (a *ArraySliceCompact[T]) Len() int {
	return a.n
}

func (a *ArraySliceCompact[T]) Value(i int) Json {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	if v, ok := a.elements[i].(Json); ok {
		return v
	}
	return nil
}

func (a *ArraySliceCompact[T]) valueImpl(i int) File {
	return a.elements[i]
}

func (a *ArraySliceCompact[T]) WriteI(w io.Writer, i int, written *int64) error {
	return arraySliceBase[*ArraySliceCompact[T]]{}.WriteI(a, w, i, written)
}

func (a *ArraySliceCompact[T]) Iterate(i int) Json {
	return a.Value(i)
}

func (a *ArraySliceCompact[T]) iterate(i int) File {
	return a.valueImpl(i)
}

func (a *ArraySliceCompact[T]) RemoveIdx(i int) Json {
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

func (a *ArraySliceCompact[T]) SetIdx(i int, j File) Json {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	a.elements[i] = j
	return a
}

func (a *ArraySliceCompact[T]) JSON() interface{} {
	return arraySliceBase[*ArraySliceCompact[T]]{}.JSON(a)
}

func (a *ArraySliceCompact[T]) AST() ast.Value {
	return arraySliceBase[*ArraySliceCompact[T]]{}.AST(a)
}

func (a *ArraySliceCompact[T]) Extract(ptr string) (Json, error) {
	return arraySliceBase[*ArraySliceCompact[T]]{}.Extract(a, ptr)
}

func (a *ArraySliceCompact[T]) extractImpl(ptr []string) (Json, error) {
	return arraySliceBase[*ArraySliceCompact[T]]{}.extractImpl(a, ptr)
}

func (a *ArraySliceCompact[T]) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, a, finder)
}

func (a *ArraySliceCompact[T]) Compare(other Json) int {
	return compare(a, other)
}

func (a *ArraySliceCompact[T]) Walk(state *DecodingState, walker Walker) {
	arrayWalk(a, state, walker)
}

func (a *ArraySliceCompact[T]) Clone(deepCopy bool) File {
	return a.clone(deepCopy)
}

func (a *ArraySliceCompact[T]) clone(deepCopy bool) Array {
	return arraySliceBase[*ArraySliceCompact[T]]{}.clone(a, deepCopy)
}

func (a *ArraySliceCompact[T]) String() string {
	return arraySliceBase[*ArraySliceCompact[T]]{}.String(a)
}
