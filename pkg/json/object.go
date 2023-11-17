package json

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/open-policy-agent/opa/ast"
)

const maxCompactObject = 32

var zeroObject = newObjectMapCompact[[0]File](map[string]File{}, nil)

type objectMapBase[T Object] struct{}

func (objectMapBase[T]) Value(o T, name string) Json {
	j := o.valueImpl(name)
	if j == nil {
		return nil
	}

	if _, ok := j.(Json); ok {
		return j.(Json)
	}

	return nil
}

func (objectMapBase[T]) JSON(o T) interface{} {
	keys := o.Names()
	object := make(map[string]interface{}, len(keys))
	for i := range keys {
		j, ok := o.iterate(i).(Json)
		if ok {
			object[keys[i]] = j.JSON()
		}
		// else: TODO: Should set the value to nil, to be identical with array processing?
	}

	return object
}

func (objectMapBase[T]) AST(o T) ast.Value {
	keys := o.Names()
	object := make([][2]*ast.Term, 0, len(keys))

	for i := range keys {
		j, ok := o.iterate(i).(Json)
		if ok {
			k := ast.String(keys[i])
			object = append(object, [2]*ast.Term{ast.NewTerm(k), ast.NewTerm(j.AST())})
		}
		// else: TODO: Should set the value to nil, to be identical with array processing?
	}

	return ast.NewObject(object...)
}

func (objectMapBase[T]) Extract(o T, ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return o.extractImpl(p)
}

func (objectMapBase[T]) extractImpl(o T, ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return o, nil
	}

	v := o.Value(ptr[0])
	if v == nil {
		return nil, errPathNotFound
	}

	return v.extractImpl(ptr[1:])
}

func (objectMapBase[T]) find(keys []string, name string) (int, bool) {
	// golang sort.Search implementation embedded here to assist compiler in inlining.
	i, j := 0, len(keys)
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j

		// begin f(h):
		ret := keys[h] >= name
		// end f(h)

		if !ret {
			i = h + 1 // preserves f(i-1) == false
		} else {
			j = h // preserves f(j) == true
		}
	}

	return i, i < len(keys) && keys[i] == name
}

func (objectMapBase[T]) WriteTo(o T, w io.Writer) (int64, error) {
	var written int64
	if err := writeSafe(w, leftCurlyBracketBytes, &written); err != nil {
		return written, err
	}

	for i, key := range o.Names() {
		if err := writeValueSeparator(w, i, &written); err != nil {
			return written, err
		}

		if data, err := marshalStringJSON(key, true); err != nil {
			return written, err
		} else if err := writeSafe(w, data, &written); err != nil {
			return written, err
		}

		if err := writeSafe(w, colonBytes, &written); err != nil {
			return written, err
		}

		if err := writeToSafe(w, o.iterate(i), &written); err != nil {
			return written, err
		}
	}

	err := writeSafe(w, rightCurlyBracketBytes, &written)
	return written, err
}

func (objectMapBase[T]) Serialize(o T, cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	properties := make([]objectEntry, o.Len())

	for i, key := range o.Names() {
		properties[i] = objectEntry{name: key, value: o.iterate(i)}
	}

	return serializeObject(properties, cache, buffer, base)
}

func (objectMapBase[T]) String(o T) string {
	names := o.Names()
	s := make([]string, 0, len(names))
	for _, name := range names {
		s = append(s, fmt.Sprint(strconv.Quote(name), ":", o.Value(name)))
	}

	return "{" + strings.Join(s, ",") + "}"
}

func (objectMapBase[T]) Union(o T, other Json) Json {
	result := NewObject2(o.Len())

	var getValue func(key string) (Json, bool)

	switch other := other.(type) {
	case Object:
		getValue = func(key string) (Json, bool) {
			v2 := other.Value(key)
			return v2, v2 != nil
		}

		for _, key := range other.Names() {
			if o.Value(key) == nil {
				result = result.Insert(NewString(key), other.Value(key))
			}
		}

	case Object2:
		getValue = func(key string) (Json, bool) {
			return other.Get(NewString(key))
		}

		other.iter(func(_ uint64, key, value Json) {
			if key, ok := key.(*String); !ok || o.Value(key.Value()) == nil {
				result = result.Insert(key, value)
			}
		})

	default:
		return other
	}

	for _, key := range o.Names() {
		v2, ok := getValue(key)
		if !ok {
			result = result.Insert(NewString(key), o.Value(key))
			continue
		}

		m := UnionObjects(o.Value(key), v2)
		result = result.Insert(NewString(key), m)
	}

	return result
}

func (objectMapBase[T]) clone(o T, internedKeys *[]string, deepCopy bool) *ObjectMap {
	values := make([]interface{}, len(*internedKeys))
	for i := 0; i < len(values); i++ {
		v := o.iterate(i)
		if deepCopy {
			v = v.Clone(true)
		}

		values[i] = v
	}

	return &ObjectMap{internedKeys, values}
}

func (objectMapBase[T]) intern(s []string, keys map[interface{}]*[]string) *[]string {
	if keys == nil {
		return &s
	}

	arr := reflect.New(reflect.ArrayOf(len(s), reflect.TypeOf(""))).Elem()
	reflect.Copy(arr, reflect.ValueOf(s))
	cmpVal := arr.Interface()

	p, ok := keys[cmpVal]
	if ok {
		return p
	}

	keys[cmpVal] = &s
	return &s
}

// ObjectMapCompact is a compact implementation of the object map for
// objects with at most 32 keys.
type ObjectMapCompact[T indexable] struct {
	internedKeys *[]string
	values       T
}

func NewObjectMapCompact(properties map[string]File, interning map[interface{}]*[]string) Object {
	if o := NewObjectMapCompactStrings(properties, interning); o != nil {
		return o
	}

	switch len(properties) {
	case 0:
		return zeroObject
	case 1:
		return newObjectMapCompact[[1]File](properties, interning)
	case 2:
		return newObjectMapCompact[[2]File](properties, interning)
	case 3:
		return newObjectMapCompact[[3]File](properties, interning)
	case 4:
		return newObjectMapCompact[[4]File](properties, interning)
	case 5:
		return newObjectMapCompact[[5]File](properties, interning)
	case 6:
		return newObjectMapCompact[[6]File](properties, interning)
	case 7:
		return newObjectMapCompact[[7]File](properties, interning)
	case 8:
		return newObjectMapCompact[[8]File](properties, interning)
	case 9:
		return newObjectMapCompact[[9]File](properties, interning)
	case 10:
		return newObjectMapCompact[[10]File](properties, interning)
	case 11:
		return newObjectMapCompact[[11]File](properties, interning)
	case 12:
		return newObjectMapCompact[[12]File](properties, interning)
	case 13:
		return newObjectMapCompact[[13]File](properties, interning)
	case 14:
		return newObjectMapCompact[[14]File](properties, interning)
	case 15:
		return newObjectMapCompact[[15]File](properties, interning)
	case 16:
		return newObjectMapCompact[[16]File](properties, interning)
	case 17:
		return newObjectMapCompact[[17]File](properties, interning)
	case 18:
		return newObjectMapCompact[[18]File](properties, interning)
	case 19:
		return newObjectMapCompact[[19]File](properties, interning)
	case 20:
		return newObjectMapCompact[[20]File](properties, interning)
	case 21:
		return newObjectMapCompact[[21]File](properties, interning)
	case 22:
		return newObjectMapCompact[[22]File](properties, interning)
	case 23:
		return newObjectMapCompact[[23]File](properties, interning)
	case 24:
		return newObjectMapCompact[[24]File](properties, interning)
	case 25:
		return newObjectMapCompact[[25]File](properties, interning)
	case 26:
		return newObjectMapCompact[[26]File](properties, interning)
	case 27:
		return newObjectMapCompact[[27]File](properties, interning)
	case 28:
		return newObjectMapCompact[[28]File](properties, interning)
	case 29:
		return newObjectMapCompact[[29]File](properties, interning)
	case 30:
		return newObjectMapCompact[[30]File](properties, interning)
	case 31:
		return newObjectMapCompact[[31]File](properties, interning)
	case 32:
		return newObjectMapCompact[[32]File](properties, interning)
	default:
		return newObjectImpl(properties, interning)
	}
}

type indexable interface {
	~[0]File |
		~[1]File |
		~[2]File |
		~[3]File |
		~[4]File |
		~[5]File |
		~[6]File |
		~[7]File |
		~[8]File |
		~[9]File |
		~[10]File |
		~[11]File |
		~[12]File |
		~[13]File |
		~[14]File |
		~[15]File |
		~[16]File |
		~[17]File |
		~[18]File |
		~[19]File |
		~[20]File |
		~[21]File |
		~[22]File |
		~[23]File |
		~[24]File |
		~[25]File |
		~[26]File |
		~[27]File |
		~[28]File |
		~[29]File |
		~[30]File |
		~[31]File |
		~[32]File
}

func newObjectMapCompact[T indexable](properties map[string]File, interning map[interface{}]*[]string) Object {
	var obj ObjectMapCompact[T]

	i := 0
	keys := make([]string, len(properties))
	for key, value := range properties {
		keys[i] = key
		obj.values[i] = value
		i++
	}

	sort.Sort(&keyValueSlicesCompact[T]{keys: keys, values: &obj.values})

	obj.internedKeys = obj.intern(keys, interning)
	return &obj
}

type keyValueSlicesCompact[T indexable] struct {
	values *T
	keys   []string
}

func (kv *keyValueSlicesCompact[T]) Len() int {
	return len(kv.keys)
}

func (kv *keyValueSlicesCompact[T]) Less(i, j int) bool {
	return kv.keys[i] < kv.keys[j]
}

func (kv *keyValueSlicesCompact[T]) Swap(i, j int) {
	kv.keys[i], kv.keys[j] = kv.keys[j], kv.keys[i]
	(*kv.values)[i], (*kv.values)[j] = (*kv.values)[j], (*kv.values)[i]
}

func (o *ObjectMapCompact[T]) WriteTo(w io.Writer) (int64, error) {
	return objectMapBase[*ObjectMapCompact[T]]{}.WriteTo(o, w)
}

func (o *ObjectMapCompact[T]) Contents() interface{} {
	return o.JSON()
}

func (o *ObjectMapCompact[T]) Names() []string {
	return o.keys()
}

func (o *ObjectMapCompact[T]) Set(name string, value Json) (Object, bool) {
	return o.setImpl(name, value)
}

func (o *ObjectMapCompact[T]) setImpl(name string, value File) (Object, bool) {
	i, ok := o.find(name)
	if ok {
		o.values[i] = value
		return o, false
	}

	n, _ := o.clone(false).setImpl(name, value)
	return n, true
}

func (o *ObjectMapCompact[T]) Value(name string) Json {
	return objectMapBase[*ObjectMapCompact[T]]{}.Value(o, name)
}

func (o *ObjectMapCompact[T]) find(name string) (int, bool) {
	return objectMapBase[*ObjectMapCompact[T]]{}.find(o.keys(), name)
}

func (o *ObjectMapCompact[T]) valueImpl(name string) File {
	i, ok := o.find(name)
	if !ok {
		return nil
	}

	return o.values[i]
}

func (o *ObjectMapCompact[T]) Iterate(i int) Json {
	return o.values[i].(Json)
}

func (o *ObjectMapCompact[T]) iterate(i int) File {
	return o.values[i]
}

func (o *ObjectMapCompact[T]) RemoveIdx(i int) Json {
	return o.clone(false).RemoveIdx(i)
}

func (o *ObjectMapCompact[T]) SetIdx(i int, j File) Json {
	keys := o.keys()
	if i < 0 || i >= len(keys) {
		panic("json: index out of range")
	}

	o.values[i] = j
	return o
}

func (o *ObjectMapCompact[T]) Remove(name string) Object {
	return o.clone(false).Remove(name)
}

func (o *ObjectMapCompact[T]) Len() int {
	return len(o.values)
}

func (o *ObjectMapCompact[T]) JSON() interface{} {
	return objectMapBase[*ObjectMapCompact[T]]{}.JSON(o)
}

func (o *ObjectMapCompact[T]) AST() ast.Value {
	return objectMapBase[*ObjectMapCompact[T]]{}.AST(o)
}

func (o *ObjectMapCompact[T]) Extract(ptr string) (Json, error) {
	return objectMapBase[*ObjectMapCompact[T]]{}.Extract(o, ptr)
}

func (o *ObjectMapCompact[T]) extractImpl(ptr []string) (Json, error) {
	return objectMapBase[*ObjectMapCompact[T]]{}.extractImpl(o, ptr)
}

func (o *ObjectMapCompact[T]) Compare(other Json) int {
	return compare(o, other)
}

func (o *ObjectMapCompact[T]) Clone(deepCopy bool) File {
	return o.clone(deepCopy)
}

func (o *ObjectMapCompact[T]) clone(deepCopy bool) *ObjectMap {
	return objectMapBase[*ObjectMapCompact[T]]{}.clone(o, o.internedKeys, deepCopy)
}

func (o *ObjectMapCompact[T]) String() string {
	return objectMapBase[*ObjectMapCompact[T]]{}.String(o)
}

func (o *ObjectMapCompact[T]) Serialize(cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	return objectMapBase[*ObjectMapCompact[T]]{}.Serialize(o, cache, buffer, base)
}

func (o *ObjectMapCompact[T]) Union(other Json) Json {
	return objectMapBase[*ObjectMapCompact[T]]{}.Union(o, other)
}

func (o *ObjectMapCompact[T]) intern(s []string, keys map[interface{}]*[]string) *[]string {
	return objectMapBase[*ObjectMapCompact[T]]{}.intern(s, keys)
}

func (o *ObjectMapCompact[T]) keys() []string {
	return *o.internedKeys
}
