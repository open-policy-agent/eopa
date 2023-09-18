package vm

import (
	"context"
	"encoding/base64"
	gojson "encoding/json"
	"errors"
	"sort"
	"strconv"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"

	"github.com/cespare/xxhash/v2"
	"github.com/open-policy-agent/opa/ast"
)

var (
	ErrInvalidData                = errors.New("invalid data")      // Unsupported data type detected.
	ErrIllegalIter                = errors.New("illegal iteration") // Illegal iteration over persisted table detected.
	_              fjson.Json     = &Set{}
	_              fjson.Json     = &Object{}
	_              IterableObject = &Object{}

	// prebuilt types are not stored as their specific types to keep them allocated as ready for interface{}.

	prebuiltTrue  fjson.Json = fjson.NewBool(true)
	prebuiltFalse fjson.Json = fjson.NewBool(false)
	prebuiltNull  fjson.Json = fjson.NewNull()
	prebuiltZero  fjson.Json = fjson.NewFloatInt(0)
	prebuiltInts  [256]fjson.Json
)

func init() {
	for i := range prebuiltInts {
		prebuiltInts[i] = fjson.NewFloatInt(int64(i))
	}
}

type (
	DataOperations struct{}

	// IterableObject is the interface for external, read-only (probably persisted) object implementations.
	IterableObject interface {
		Get(ctx context.Context, key interface{}) (interface{}, bool, error)
		Iter(ctx context.Context, f func(key, value interface{}) bool) error
		Len(ctx context.Context) (int, error)
		Hash(ctx context.Context) (uint64, error)
	}

	GetCallNamespace interface {
		GetCall(ctx context.Context, key interface{}) (interface{}, bool, error)
	}

	CallNamespace interface {
		Call(ctx context.Context, args []*interface{}, caller *State) (interface{}, bool, bool, error)
	}
)

func (*DataOperations) Get(ctx context.Context, value, key interface{}) (interface{}, bool, error) {
	jkey, err := castJSON(ctx, key)
	if err != nil {
		return nil, false, err
	}

	switch v := value.(type) {
	case fjson.Array:
		n, ok := jkey.(fjson.Float)
		if !ok {
			return nil, false, nil
		}

		i, err := n.Value().Int64()
		if err != nil {
			return nil, false, nil
		}

		if i < 0 || i >= int64(v.Len()) {
			return nil, false, nil
		}

		return v.Iterate(int(i)), true, nil

	case fjson.Object:
		s, ok := jkey.(*fjson.String)
		if !ok {
			return nil, false, nil
		}

		value := v.Value(s.Value())
		return value, value != nil, nil

	case *Set:
		return v.Get(ctx, jkey)

	case IterableObject:
		return v.Get(ctx, jkey)

	default:
		if _, err := castJSON(ctx, value); err != nil {
			return nil, false, err
		}
	}

	return nil, false, nil
}

func (*DataOperations) GetCall(ctx context.Context, value, key interface{}) (interface{}, bool, error) {
	jkey, err := castJSON(ctx, key)
	if err != nil {
		return nil, false, err
	}

	switch v := value.(type) {
	case GetCallNamespace:
		return v.GetCall(ctx, jkey)
	}

	return nil, false, nil
}

func (*DataOperations) IsCall(value interface{}) (bool, error) {
	switch value.(type) {
	case CallNamespace:
		return true, nil
	default:
		return false, nil
	}
}

func (*DataOperations) Call(ctx context.Context, value interface{}, args []*interface{}, caller *State) (interface{}, bool, bool, error) {
	switch v := value.(type) {
	case CallNamespace:
		return v.Call(ctx, args, caller)
	}

	return nil, false, false, nil
}

func (*DataOperations) ArrayAppend(ctx context.Context, array interface{}, value interface{}) (interface{}, bool, error) {
	jvalue, err := castJSON(ctx, value)
	if err != nil {
		return nil, false, err
	}

	switch a := array.(type) {
	case fjson.Array:
		// TODO: Avoiding castJSON would delay the reading of iterable object.

		// Using singular version avoids an allocation to construct slice of arguments.
		b, ok := a.AppendSingle(jvalue)
		return b, ok, nil
	default:
		if _, err := castJSON(ctx, array); err != nil {
			return nil, false, err
		}
	}

	return array, false, nil
}

func (*DataOperations) CopyShallow(ctx context.Context, value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case fjson.Null:
		return v, nil
	case fjson.Bool:
		return v, nil
	case fjson.Float:
		return v, nil
	case *fjson.String:
		return v, nil
	case fjson.Array:
		return v.Clone(false), nil
	case fjson.Object:
		return v.Clone(false), nil
	case *Set:
		set := NewSet()
		var err error
		v.Iter(func(v fjson.Json) bool {
			err = set.Add(ctx, v)
			return err != nil
		})
		return set, err
	case IterableObject:
		// TODO: Return a copy-on-write object.
		obj := NewObject()
		var err2 error
		err := v.Iter(ctx, func(k, v interface{}) bool {
			err2 = obj.Insert(ctx, k, v)
			return err2 != nil
		})
		if err2 != nil {
			return nil, err2
		} else if err != nil {
			return nil, err
		}

		return obj, nil
	default:
		notImplemented()
	}

	return nil, nil
}

func (*DataOperations) Equal(ctx context.Context, a, b interface{}) (bool, error) {
	return equalOp(ctx, a, b)
}

// FromInterface converts a golang native data to internal representation.
func (o *DataOperations) FromInterface(ctx context.Context, x interface{}) (interface{}, error) {
	switch x := x.(type) {
	case nil:
		return fjson.NewNull(), nil
	case ast.Null:
		return fjson.NewNull(), nil
	case bool:
		return fjson.NewBool(x), nil
	case ast.Boolean:
		return fjson.NewBool(bool(x)), nil
	case gojson.Number:
		return fjson.NewFloat(x), nil
	case ast.Number:
		return fjson.NewFloat(gojson.Number(x)), nil
	case int64:
		return o.MakeNumberInt(x), nil
	case uint64:
		notImplemented()
	case float64:
		return o.MakeNumberFloat(x), nil
	case int:
		return o.MakeNumberInt(int64(x)), nil
	case string:
		return fjson.NewString(x), nil
	case ast.String:
		return fjson.NewString(string(x)), nil
	case []string:
		values := make([]fjson.File, 0, len(x))
		for _, v := range x {
			values = append(values, fjson.NewString(v))
		}
		return fjson.NewArray(values...), nil
	case []interface{}:
		values := make([]fjson.File, 0, len(x))
		for _, v := range x {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return nil, err
			}
			values = append(values, y.(fjson.Json))
		}
		return fjson.NewArray(values...), nil
	case *ast.Array:
		values := make([]fjson.File, 0, x.Len())
		err := x.Iter(func(v *ast.Term) error {
			y, err := o.FromInterface(ctx, v.Value)
			if err != nil {
				return err
			}
			values = append(values, y.(fjson.Json))
			return nil
		})
		if err != nil {
			return nil, err
		}
		return fjson.NewArray(values...), nil
	case map[string]interface{}:
		obj := NewObject()
		for k, v := range x {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return nil, err
			}
			if err := obj.Insert(ctx, fjson.NewString(k), y.(fjson.Json)); err != nil {
				return nil, err
			}
		}
		return obj, nil
	case []map[string]interface{}:
		values := make([]fjson.File, 0, len(x))
		for _, v := range x {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return nil, err
			}
			values = append(values, y.(fjson.Json))
		}
		return fjson.NewArray(values...), nil
	case map[string]string:
		obj := NewObject()
		for k, v := range x {
			if err := obj.Insert(ctx, fjson.NewString(k), fjson.NewString(v)); err != nil {
				return nil, err
			}
		}
		return obj, nil
	case ast.Object:
		obj := NewObject()
		if err := x.Iter(func(k, v *ast.Term) error {
			kj, err := o.FromInterface(ctx, k)
			if err != nil {
				return err
			}
			vj, err := o.FromInterface(ctx, v)
			if err != nil {
				return err
			}
			return obj.Insert(ctx, kj.(fjson.Json), vj.(fjson.Json))
		}); err != nil {
			return nil, err
		}
		return obj, nil
	case ast.Ref:
		notImplemented()
	case ast.Set:
		set := NewSet()
		err := x.Iter(func(v *ast.Term) error {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return err
			}
			return set.Add(ctx, y.(fjson.Json))
		})
		if err != nil {
			return nil, err
		}
		return set, nil
	case *ast.Term:
		return o.FromInterface(ctx, x.Value)
	case []byte:
		return fjson.NewString(base64.StdEncoding.EncodeToString(x)), nil
	default:
		notImplemented()
	}

	return nil, nil
}

func (o *DataOperations) Iter(ctx context.Context, v interface{}, f func(key, value interface{}) bool) error {
	switch v := v.(type) {
	case fjson.Array:
		n := v.Len()
		for i := 0; i < n; i++ {
			if f(o.MakeNumberInt(int64(i)), v.Iterate(i)) {
				break
			}
		}
	case fjson.Object:
		for i, key := range v.Names() {
			if f(fjson.NewString(key), v.Iterate(i)) {
				break
			}
		}
	case *Set:
		v.Iter(func(v fjson.Json) bool {
			return f(v, v)
		})
	case IterableObject:
		return v.Iter(ctx, f)
	}

	return nil
}

func (*DataOperations) IsArray(ctx context.Context, v interface{}) (bool, error) {
	switch v.(type) {
	case fjson.Array:
		return true, nil
	case IterableObject:
		return false, nil
	default:
		_, err := castJSON(ctx, v)
		return false, err
	}
}

func (*DataOperations) IsObject(ctx context.Context, v interface{}) (bool, error) {
	switch v.(type) {
	case fjson.Object, IterableObject:
		return true, nil
	default:
		_, err := castJSON(ctx, v)
		return false, err
	}
}

func (*DataOperations) MakeArray(int32) fjson.Array {
	return fjson.NewArray()
}

func (*DataOperations) MakeBoolean(v bool) fjson.Json {
	if v {
		return prebuiltTrue
	}

	return prebuiltFalse
}

func (*DataOperations) MakeObject() *Object {
	return NewObject()
}

func (*DataOperations) MakeNull() fjson.Json {
	return prebuiltNull
}

func (*DataOperations) MakeNumberFloat(f float64) fjson.Float {
	return fjson.NewFloat(gojson.Number(strconv.FormatFloat(f, 'g', -1, 64)))
}

func (o *DataOperations) MakeNumberZero() fjson.Json {
	return prebuiltZero
}

func (o *DataOperations) MakeNumberInt(i int64) fjson.Json {
	if i >= 0 && i < int64(len(prebuiltInts)) {
		return prebuiltInts[i]
	}

	return fjson.NewFloat(gojson.Number(strconv.FormatInt(i, 10)))
}

func (*DataOperations) MakeNumberRef(n interface{}) fjson.Float {
	return fjson.NewFloat(gojson.Number(n.(*fjson.String).Value()))
}

func (*DataOperations) MakeSet() *Set {
	return NewSet()
}

func (*DataOperations) MakeString(v string) *fjson.String {
	return fjson.NewString(v)
}

func (o *DataOperations) Len(ctx context.Context, v interface{}) (fjson.Json, error) {
	switch v := v.(type) {
	case fjson.Array:
		return o.MakeNumberInt(int64(v.Len())), nil
	case fjson.Object:
		return o.MakeNumberInt(int64(v.Len())), nil
	case *Set:
		return o.MakeNumberInt(int64(v.Len())), nil
	case *fjson.String:
		return o.MakeNumberInt(int64(len(v.Value()))), nil
	case IterableObject:
		n, err := v.Len(ctx)
		if err != nil {
			return nil, err
		}

		return o.MakeNumberInt(int64(n)), nil
	default:
		if _, err := castJSON(ctx, v); err != nil {
			return o.MakeNumberZero(), err
		}

		return o.MakeNumberZero(), nil
	}
}

func (*DataOperations) ObjectGet(ctx context.Context, object, key interface{}) (interface{}, bool, error) {
	jkey, err := castJSON(ctx, key)
	if err != nil {
		return nil, false, err
	}

	switch object := object.(type) {
	case IterableObject:
		value, ok, err := object.Get(ctx, jkey)
		return value, ok, err

	case fjson.Object:
		s, ok := jkey.(*fjson.String)
		if !ok {
			return nil, false, nil
		}

		value := object.Value(s.Value())
		return value, value != nil, nil
	}

	return nil, false, nil
}

func (*DataOperations) ObjectInsert(ctx context.Context, object, key, value interface{}) (interface{}, bool, error) {
	jkey, err := castJSON(ctx, key)
	if err != nil {
		return nil, false, err
	}

	switch o := object.(type) {
	case *Object:
		return object, false, o.Insert(ctx, jkey, value)

	case fjson.Object:
		jvalue, err := castJSON(ctx, value)
		if err != nil {
			return nil, false, err
		}

		s, ok := jkey.(*fjson.String)
		if !ok {
			// Evaluation should never try to modify a JSON it loaded from disk.
			panic("not reached")
		}

		o, ok = o.Set(s.Value(), jvalue)
		return o, ok, nil

	default:
		_, err := castJSON(ctx, object)
		if err != nil {
			return nil, false, err
		}

		return object, false, nil
	}
}

func (o *DataOperations) ObjectMerge(ctx context.Context, a, b interface{}) (interface{}, error) {
	_, okaa := a.(fjson.Object)
	_, okab := a.(IterableObject)
	_, okba := b.(fjson.Object)
	_, okbb := b.(IterableObject)

	if (!okaa && !okab) || (!okba && !okbb) {
		return a, nil
	}

	merged := NewObject()

	var err error

	if err2 := objectIterate(ctx, a, func(key, value interface{}) bool {
		var other interface{}
		var ok bool
		other, ok, err = objectGet(ctx, b, key)
		if err != nil {
			return true
		} else if !ok {
			err = merged.Insert(ctx, key, value)
			return err != nil
		}

		var m interface{}
		m, err = o.ObjectMerge(ctx, value, other)
		if err != nil {
			return true
		}

		err = merged.Insert(ctx, key, m)
		return err != nil
	}); err2 != nil {
		return nil, err2
	} else if err != nil {
		return nil, err
	}

	if err2 := objectIterate(ctx, b, func(key, value interface{}) bool {
		var ok bool
		_, ok, err = objectGet(ctx, a, key)
		if err != nil {
			return true
		}
		if !ok {
			err = merged.Insert(ctx, key, value)
			if err != nil {
				return true
			}
		}
		return false
	}); err2 != nil {
		return nil, err2
	} else if err != nil {
		return nil, err
	}

	return merged, nil
}

func objectIterate(ctx context.Context, obj interface{}, f func(key, value interface{}) bool) error {
	if obj, ok := obj.(fjson.Object); ok {
		for i, name := range obj.Names() {
			if f(fjson.NewString(name), obj.Iterate(i)) {
				break
			}
		}

		return nil
	}

	return obj.(IterableObject).Iter(ctx, f)
}

func objectGet(ctx context.Context, obj interface{}, key interface{}) (interface{}, bool, error) {
	if obj, ok := obj.(fjson.Object); ok {
		if s, ok := key.(*fjson.String); ok {
			value := obj.Value(s.Value())
			return value, value != nil, nil
		}

		return nil, false, nil
	}

	return obj.(IterableObject).Get(ctx, key)
}

func (*DataOperations) SetAdd(ctx context.Context, set, value interface{}) error {
	jvalue, err := castJSON(ctx, value)
	if err != nil {
		return err
	}

	switch set := set.(type) {
	case *Set:
		return set.Add(ctx, jvalue)
	default:
		if _, err := castJSON(ctx, set); err != nil {
			return err
		}
	}

	return nil
}

func (o *DataOperations) ToAST(ctx context.Context, v interface{}) (ast.Value, error) {
	switch v := v.(type) {
	case fjson.Null:
		return ast.Null{}, nil

	case fjson.Bool:
		return ast.Boolean(v.Value()), nil

	case fjson.Float:
		return ast.Number(string(v.Value())), nil

	case *fjson.String:
		return ast.String(v.Value()), nil

	case fjson.Array:
		n := v.Len()
		terms := make([]*ast.Term, 0, n)
		for i := 0; i < n; i++ {
			x := v.Iterate(i)
			a, err := o.ToAST(ctx, x)
			if err != nil {
				return nil, err
			}

			terms = append(terms, ast.NewTerm(a))
		}
		arr := ast.NewArray(terms...)
		return arr, nil

	case fjson.Object:
		var err error

		names := v.Names()
		terms := make([][2]*ast.Term, 0, len(names))
		for i, k := range names {
			v := v.Iterate(i)
			a := ast.String(k)

			var b ast.Value
			b, err = o.ToAST(ctx, v)
			if err != nil {
				break
			}

			terms = append(terms, [2]*ast.Term{ast.NewTerm(a), ast.NewTerm(b)})
		}
		obj := ast.NewObject(terms...)
		if err != nil {
			return nil, err
		}
		return obj, nil

	case *Set:
		// TODO: Sorting is for deterministic tests.
		// We prealloc the Term array and sort it here to trim down the total number of allocs.
		var err error
		terms := make([]*ast.Term, 0, v.Len())
		v.Iter(func(v fjson.Json) bool {
			var a ast.Value
			a, err = o.ToAST(ctx, v)
			if err != nil {
				return true
			}

			terms = append(terms, ast.NewTerm(a))
			return false
		})
		if err != nil {
			return nil, err
		}

		sort.Sort(termSlice(terms))
		set := ast.NewSet(terms...)
		return set, nil

	case IterableObject:
		obj := ast.NewObject()
		var err error
		if err2 := v.Iter(ctx, func(k, v interface{}) bool {
			var a ast.Value
			a, err = o.ToAST(ctx, k)
			if err != nil {
				return true
			}

			var b ast.Value
			b, err = o.ToAST(ctx, v)
			if err != nil {
				return true
			}

			obj.Insert(ast.NewTerm(a), ast.NewTerm(b))
			return false
		}); err2 != nil {
			return nil, err2
		} else if err != nil {
			return nil, err
		}
		return obj, nil
	}

	notImplemented()
	return nil, nil
}

// ToInterface converts the data to golang native presentation.
func (o *DataOperations) ToInterface(ctx context.Context, v interface{}) (interface{}, error) {
	v, err := o.ToAST(ctx, v)
	if err != nil {
		return nil, err
	}

	return ast.JSONWithOpt(v.(ast.Value), ast.JSONOpt{SortSets: false})
}

// Example JSON type library

type Set struct {
	fjson.Json
	set HashSet
}

func NewSet() *Set {
	return &Set{set: *NewHashSet()}
}

func (s *Set) Add(ctx context.Context, v fjson.Json) error {
	return s.set.Put(ctx, v)
}

func (s *Set) Get(ctx context.Context, k fjson.Json) (fjson.Json, bool, error) {
	ok, err := s.set.Get(ctx, k)
	if err != nil {
		return nil, false, err
	} else if ok {
		return k, true, nil
	}

	return nil, false, nil
}

func (s *Set) Iter(iter func(v fjson.Json) bool) bool {
	// Note: this does not return the elements in any sorted order.
	return s.set.Iter(func(v interface{}) bool {
		return iter(v.(fjson.Json))
	})
}

func (s *Set) Len() int {
	return s.set.Len()
}

func (s *Set) Equal(ctx context.Context, y *Set) (bool, error) {
	if s == y {
		return true, nil
	}

	if s.Len() != y.Len() {
		return false, nil
	}

	match := true
	var err error
	s.Iter(func(v fjson.Json) bool {
		_, match, err = y.Get(ctx, v)
		if err != nil {
			return true
		}
		return !match
	})
	if err != nil {
		return false, err
	}

	return match, nil
}

func (s *Set) Hash(ctx context.Context) (uint64, error) {
	var (
		m   uint64
		err error
	)
	s.set.Iter(func(v interface{}) bool {
		h := xxhash.New()
		err = hashImpl(ctx, v, h)
		m += h.Sum64()
		return err != nil
	})

	return m, err
}

type Object struct {
	fjson.Json
	obj HashMap
}

func NewObject() *Object {
	return &Object{obj: *NewHashMap()}
}

func (o *Object) Insert(ctx context.Context, k, v interface{}) error {
	return o.obj.Put(ctx, k, v)
}

func (o *Object) Get(ctx context.Context, k interface{}) (interface{}, bool, error) {
	return o.obj.Get(ctx, k)
}

func (o *Object) Iter(_ context.Context, iter func(key, value interface{}) bool) error {
	o.obj.Iter(func(k, v T) bool {
		return iter(k, v)
	})
	return nil
}

func (o *Object) Len(context.Context) (int, error) {
	return o.obj.Len(), nil
}

func (o *Object) Hash(ctx context.Context) (uint64, error) {
	var (
		err error
		h   uint64
	)
	o.obj.Iter(func(k, v T) bool {
		h, err = ObjectHashEntry(ctx, h, k, v)
		return err != nil
	})

	return h, err
}

func castJSON(ctx context.Context, v interface{}) (fjson.Json, error) {
	j, ok := v.(fjson.Json)
	if ok {
		return j, nil
	}

	switch v := v.(type) {
	case IterableObject:
		obj := NewObject()
		var err error
		v.Iter(ctx, func(k, v interface{}) bool {
			obj.Insert(ctx, k.(*fjson.String), v.(fjson.Json))
			return false
		})
		if err != nil {
			return nil, err
		}
		return obj, nil
	}

	return nil, ErrInvalidData
}

func notImplemented() {
	panic("not implemented")
}

// Borrowed definitions from OPA's ast/term to allow using Golang's default sort on term slices.
type termSlice []*ast.Term

func (s termSlice) Less(i, j int) bool { return ast.Compare(s[i].Value, s[j].Value) < 0 }
func (s termSlice) Swap(i, j int)      { x := s[i]; s[i] = s[j]; s[j] = x }
func (s termSlice) Len() int           { return len(s) }
