package json

import (
	"bytes"
	"io"
	"sort"

	"github.com/open-policy-agent/opa/v1/ast"
)

// ObjectMapCompact is a compact implementation of the object map for
// objects with at most 32 String values.
type ObjectMapCompactStrings[T indexableStrings] struct {
	internedKeys *[]string
	values       T
}

type indexableStrings interface {
	[1]*String |
		[2]*String |
		[3]*String |
		[4]*String |
		[5]*String |
		[6]*String |
		[7]*String |
		[8]*String |
		[9]*String |
		[10]*String |
		[11]*String |
		[12]*String |
		[13]*String |
		[14]*String |
		[15]*String |
		[16]*String |
		[17]*String |
		[18]*String |
		[19]*String |
		[20]*String |
		[21]*String |
		[22]*String |
		[23]*String |
		[24]*String |
		[25]*String |
		[26]*String |
		[27]*String |
		[28]*String |
		[29]*String |
		[30]*String |
		[31]*String |
		[32]*String
}

func NewObjectMapCompactStrings(properties map[string]File, interning map[interface{}]*[]string) Object {
	n := len(properties)
	if n == 0 || n > 32 {
		return nil
	}

	for _, f := range properties {
		switch f.(type) {
		case *String:
		default:
			return nil
		}
	}

	switch n {
	case 1:
		return newObjectMapCompactStrings[[1]*String](properties, interning)
	case 2:
		return newObjectMapCompactStrings[[2]*String](properties, interning)
	case 3:
		return newObjectMapCompactStrings[[3]*String](properties, interning)
	case 4:
		return newObjectMapCompactStrings[[4]*String](properties, interning)
	case 5:
		return newObjectMapCompactStrings[[5]*String](properties, interning)
	case 6:
		return newObjectMapCompactStrings[[6]*String](properties, interning)
	case 7:
		return newObjectMapCompactStrings[[7]*String](properties, interning)
	case 8:
		return newObjectMapCompactStrings[[8]*String](properties, interning)
	case 9:
		return newObjectMapCompactStrings[[9]*String](properties, interning)
	case 10:
		return newObjectMapCompactStrings[[10]*String](properties, interning)
	case 11:
		return newObjectMapCompactStrings[[11]*String](properties, interning)
	case 12:
		return newObjectMapCompactStrings[[12]*String](properties, interning)
	case 13:
		return newObjectMapCompactStrings[[13]*String](properties, interning)
	case 14:
		return newObjectMapCompactStrings[[14]*String](properties, interning)
	case 15:
		return newObjectMapCompactStrings[[15]*String](properties, interning)
	case 16:
		return newObjectMapCompactStrings[[16]*String](properties, interning)
	case 17:
		return newObjectMapCompactStrings[[17]*String](properties, interning)
	case 18:
		return newObjectMapCompactStrings[[18]*String](properties, interning)
	case 19:
		return newObjectMapCompactStrings[[19]*String](properties, interning)
	case 20:
		return newObjectMapCompactStrings[[20]*String](properties, interning)
	case 21:
		return newObjectMapCompactStrings[[21]*String](properties, interning)
	case 22:
		return newObjectMapCompactStrings[[22]*String](properties, interning)
	case 23:
		return newObjectMapCompactStrings[[23]*String](properties, interning)
	case 24:
		return newObjectMapCompactStrings[[24]*String](properties, interning)
	case 25:
		return newObjectMapCompactStrings[[25]*String](properties, interning)
	case 26:
		return newObjectMapCompactStrings[[26]*String](properties, interning)
	case 27:
		return newObjectMapCompactStrings[[27]*String](properties, interning)
	case 28:
		return newObjectMapCompactStrings[[28]*String](properties, interning)
	case 29:
		return newObjectMapCompactStrings[[29]*String](properties, interning)
	case 30:
		return newObjectMapCompactStrings[[30]*String](properties, interning)
	case 31:
		return newObjectMapCompactStrings[[31]*String](properties, interning)
	case 32:
		return newObjectMapCompactStrings[[32]*String](properties, interning)
	}

	panic("not reached")
}

func newObjectMapCompactStrings[T indexableStrings](properties map[string]File, interning map[interface{}]*[]string) Object {
	var obj ObjectMapCompactStrings[T]

	i := 0
	keys := make([]string, len(properties))
	for key, value := range properties {
		keys[i] = key
		obj.values[i] = value.(*String)
		i++
	}

	sort.Sort(&keyValueSlicesCompactStrings[T]{keys: keys, values: &obj.values})

	obj.internedKeys = obj.intern(keys, interning)
	return &obj
}

type keyValueSlicesCompactStrings[T indexableStrings] struct {
	values *T
	keys   []string
}

func (kv *keyValueSlicesCompactStrings[T]) Len() int {
	return len(kv.keys)
}

func (kv *keyValueSlicesCompactStrings[T]) Less(i, j int) bool {
	return kv.keys[i] < kv.keys[j]
}

func (kv *keyValueSlicesCompactStrings[T]) Swap(i, j int) {
	kv.keys[i], kv.keys[j] = kv.keys[j], kv.keys[i]
	(*kv.values)[i], (*kv.values)[j] = (*kv.values)[j], (*kv.values)[i]
}

func (o *ObjectMapCompactStrings[T]) WriteTo(w io.Writer) (int64, error) {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.WriteTo(o, w)
}

func (o *ObjectMapCompactStrings[T]) Contents() interface{} {
	return o.JSON()
}

func (o *ObjectMapCompactStrings[T]) Names() []string {
	return o.keys()
}

func (o *ObjectMapCompactStrings[T]) Set(name string, value Json) (Object, bool) {
	return o.setImpl(name, value)
}

func (o *ObjectMapCompactStrings[T]) setImpl(name string, value File) (Object, bool) {
	i, ok := o.find(name)
	if ok {
		if s, ok := value.(*String); ok {
			o.values[i] = s
			return o, false
		}
	}

	n, _ := o.clone().setImpl(name, value)
	return n, true
}

func (o *ObjectMapCompactStrings[T]) Value(name string) Json {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.Value(o, name)
}

func (o *ObjectMapCompactStrings[T]) find(name string) (int, bool) {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.find(o.keys(), name)
}

func (o *ObjectMapCompactStrings[T]) valueImpl(name string) File {
	i, ok := o.find(name)
	if !ok {
		return nil
	}

	return o.values[i]
}

func (o *ObjectMapCompactStrings[T]) Iterate(i int) Json {
	return o.values[i]
}

func (o *ObjectMapCompactStrings[T]) iterate(i int) File {
	return o.values[i]
}

func (o *ObjectMapCompactStrings[T]) RemoveIdx(i int) Json {
	return o.clone().RemoveIdx(i)
}

func (o *ObjectMapCompactStrings[T]) SetIdx(i int, j File) Json {
	keys := o.keys()
	if i < 0 || i >= len(keys) {
		panic("json: index out of range")
	}

	if s, ok := j.(*String); ok {
		o.values[i] = s
		return o
	}

	return o.clone().SetIdx(i, j)
}

func (o *ObjectMapCompactStrings[T]) Remove(name string) Object {
	return o.clone().Remove(name)
}

func (o *ObjectMapCompactStrings[T]) Len() int {
	return len(o.values)
}

func (o *ObjectMapCompactStrings[T]) JSON() interface{} {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.JSON(o)
}

func (o *ObjectMapCompactStrings[T]) AST() ast.Value {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.AST(o)
}

func (o *ObjectMapCompactStrings[T]) Extract(ptr string) (Json, error) {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.Extract(o, ptr)
}

func (o *ObjectMapCompactStrings[T]) extractImpl(ptr []string) (Json, error) {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.extractImpl(o, ptr)
}

func (o *ObjectMapCompactStrings[T]) Compare(other Json) int {
	return compare(o, other)
}

func (o *ObjectMapCompactStrings[T]) Clone(bool) File {
	return o.clone()
}

func (o *ObjectMapCompactStrings[T]) clone() *ObjectMap {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.clone(o, o.internedKeys, false)
}

func (o *ObjectMapCompactStrings[T]) String() string {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.String(o)
}

func (o *ObjectMapCompactStrings[T]) Serialize(cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.Serialize(o, cache, buffer, base)
}

func (o *ObjectMapCompactStrings[T]) Union(other Json) Json {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.Union(o, other)
}

func (o *ObjectMapCompactStrings[T]) intern(s []string, keys map[interface{}]*[]string) *[]string {
	return objectMapBase[*ObjectMapCompactStrings[T]]{}.intern(s, keys)
}

func (o *ObjectMapCompactStrings[T]) keys() []string {
	return *o.internedKeys
}
