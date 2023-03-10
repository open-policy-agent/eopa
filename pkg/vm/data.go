package vm

import (
	"context"
	"encoding/base64"
	gojson "encoding/json"
	"errors"
	"math/big"
	"sort"
	"strconv"

	fjson "github.com/styrainc/load-private/pkg/json"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/util"
)

var (
	ErrInvalidData            = errors.New("invalid data")      // Unsupported data type detected.
	ErrIllegalIter            = errors.New("illegal iteration") // Illegal iteration over persisted table detected.
	_              fjson.Json = &Set{}
	_              fjson.Json = &Object{}
)

type (
	dataOperations struct{}

	// GetNamespace is the interface external, read-only, non-scannable namespace implementation should provide.
	GetNamespace interface {
		Get(ctx context.Context, key interface{}) (interface{}, bool, error)
	}

	IterNamespace interface {
		Iter(ctx context.Context, f func(key, value interface{}) bool) error
	}

	GetCallNamespace interface {
		GetCall(ctx context.Context, key interface{}) (interface{}, bool, error)
	}

	CallNamespace interface {
		Call(ctx context.Context, args []*interface{}, caller *State) (interface{}, bool, bool, error)
	}
)

func (*dataOperations) Get(ctx context.Context, value, key interface{}) (interface{}, bool, error) {
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

	case *Object:
		value, ok := v.Get(jkey)
		return value, ok, nil

	case fjson.Object:
		s, ok := jkey.(fjson.String)
		if !ok {
			return nil, false, nil
		}

		value := v.Value(s.Value())
		return value, value != nil, nil

	case *Set:
		match, ok := v.Get(jkey)
		return match, ok, nil

	case GetNamespace:
		s, ok := jkey.(fjson.String)
		if !ok {
			return nil, false, nil
		}

		return v.Get(ctx, s)

	default:
		if _, err := castJSON(ctx, value); err != nil {
			return nil, false, err
		}
	}

	return nil, false, nil
}

func (*dataOperations) GetCall(ctx context.Context, value, key interface{}) (interface{}, bool, error) {
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

func (*dataOperations) IsCall(value interface{}) (bool, error) {
	switch value.(type) {
	case CallNamespace:
		return true, nil
	default:
		return false, nil
	}
}

func (*dataOperations) Call(ctx context.Context, value interface{}, args []*interface{}, caller *State) (interface{}, bool, bool, error) {
	switch v := value.(type) {
	case CallNamespace:
		return v.Call(ctx, args, caller)
	}

	return nil, false, false, nil
}

func (*dataOperations) ArrayAppend(ctx context.Context, array interface{}, value interface{}) (interface{}, error) {
	jvalue, err := castJSON(ctx, value)
	if err != nil {
		return nil, err
	}

	switch a := array.(type) {
	case fjson.Array:
		a.Append(jvalue)
		return a, nil
	default:
		if _, err := castJSON(ctx, array); err != nil {
			return nil, err
		}
	}

	return array, nil
}

func (*dataOperations) CopyShallow(value interface{}) interface{} {
	switch v := value.(type) {
	case fjson.Null:
		return v
	case fjson.Bool:
		return v
	case fjson.Float:
		return v
	case fjson.String:
		return v
	case fjson.Array:
		return v.Clone(false)
	case *Object:
		obj := NewObject()
		v.Iter(func(key, value fjson.Json) bool {
			obj.Insert(key, value)
			return false
		})

		return obj

	case fjson.Object:
		return v.Clone(false)
	case *Set:
		set := NewSet()
		v.Iter(func(v fjson.Json) bool {
			set.Add(v)
			return false
		})
		return set
	case GetNamespace, IterNamespace:
		return v // TODO: return a copy?
	default:
		notImplemented()
	}

	return nil
}

func (*dataOperations) Equal(ctx context.Context, a, b interface{}) (bool, error) {
	x, err := castJSON(ctx, a)
	if err != nil {
		return false, err
	}

	y, err := castJSON(ctx, b)
	if err != nil {
		return false, err
	}

	return equalOp(x, y), nil
}

// FromInterface converts a golang native data to internal representation.
func (o *dataOperations) FromInterface(x interface{}) interface{} {
	switch x := x.(type) {
	case nil:
		return fjson.NewNull()
	case ast.Null:
		return fjson.NewNull()
	case bool:
		return fjson.NewBool(x)
	case ast.Boolean:
		return fjson.NewBool(bool(x))
	case gojson.Number:
		return fjson.NewFloat(x)
	case ast.Number:
		return fjson.NewFloat(gojson.Number(x))
	case int64:
		return o.MakeNumberInt(x)
	case uint64:
		notImplemented()
	case float64:
		return o.MakeNumberFloat(x)
	case int:
		return o.MakeNumberInt(int64(x))
	case string:
		return fjson.NewString(x)
	case ast.String:
		return fjson.NewString(string(x))
	case []interface{}:
		values := make([]fjson.File, 0, len(x))
		for _, v := range x {
			values = append(values, o.FromInterface(v).(fjson.Json))
		}
		return fjson.NewArray(values...)
	case *ast.Array:
		values := make([]fjson.File, 0, x.Len())
		x.Iter(func(v *ast.Term) error {
			values = append(values, o.FromInterface(v.Value).(fjson.Json))
			return nil
		})
		return fjson.NewArray(values...)
	case map[string]interface{}:
		obj := NewObject()
		for k, v := range x {
			obj.Insert(fjson.NewString(k), o.FromInterface(v).(fjson.Json))
		}
		return obj
	case []map[string]interface{}:
		values := make([]fjson.File, 0, len(x))
		for _, v := range x {
			values = append(values, o.FromInterface(v).(fjson.Json))
		}
		return fjson.NewArray(values...)
	case map[string]string:
		obj := NewObject()
		for k, v := range x {
			obj.Insert(fjson.NewString(k), fjson.NewString(v))
		}
		return obj
	case ast.Object:
		obj := NewObject()
		x.Iter(func(k, v *ast.Term) error {
			obj.Insert(o.FromInterface(k).(fjson.Json), o.FromInterface(v).(fjson.Json))
			return nil
		})
		return obj
	case ast.Ref:
		notImplemented()
	case ast.Set:
		set := NewSet()
		x.Iter(func(v *ast.Term) error {
			set.Add(o.FromInterface(v).(fjson.Json))
			return nil
		})
		return set
	case *ast.Term:
		return o.FromInterface(x.Value)
	case []byte:
		return fjson.NewString(base64.StdEncoding.EncodeToString(x))
	default:
		notImplemented()
	}

	return nil
}

func (o *dataOperations) Iter(ctx context.Context, v interface{}, f func(key, value interface{}) bool) error {
	switch v := v.(type) {
	case fjson.Array:
		n := v.Len()
		for i := 0; i < n; i++ {
			if f(o.MakeNumberInt(int64(i)), v.Iterate(i)) {
				break
			}
		}
	case *Object:
		v.Iter(func(key, value fjson.Json) bool {
			return f(key, value)
		})
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
	case IterNamespace:
		return v.Iter(ctx, f)
	}

	return nil
}

func (*dataOperations) IsArray(ctx context.Context, v interface{}) (bool, error) {
	switch v.(type) {
	case fjson.Array:
		return true, nil
	case IterNamespace:
		return false, nil
	default:
		_, err := castJSON(ctx, v)
		return false, err
	}
}

func (*dataOperations) IsObject(ctx context.Context, v interface{}) (bool, error) {
	switch v.(type) {
	case fjson.Object, *Object:
		return true, nil
	case IterNamespace:
		return true, nil
	default:
		_, err := castJSON(ctx, v)
		return false, err
	}
}

func (*dataOperations) MakeArray(capacity int32) interface{} {
	return fjson.NewArray()
}

func (*dataOperations) MakeBoolean(v bool) interface{} {
	return fjson.NewBool(v)
}

func (*dataOperations) MakeObject() interface{} {
	return NewObject()
}

func (*dataOperations) MakeNull() interface{} {
	return fjson.NewNull()
}

func (*dataOperations) MakeNumberFloat(f float64) interface{} {
	return fjson.NewFloat(gojson.Number(strconv.FormatFloat(f, 'g', -1, 64)))
}

func (*dataOperations) MakeNumberInt(i int64) interface{} {
	return fjson.NewFloat(gojson.Number(strconv.FormatInt(i, 10)))
}

func (*dataOperations) MakeNumberRef(n interface{}) interface{} {
	return fjson.NewFloat(gojson.Number(n.(fjson.String).Value()))
}

func (*dataOperations) MakeSet() interface{} {
	return NewSet()
}

func (*dataOperations) MakeString(v string) interface{} {
	return fjson.NewString(v)
}

func (o *dataOperations) Len(ctx context.Context, v interface{}) (interface{}, error) {
	switch v := v.(type) {
	case fjson.Array:
		return o.MakeNumberInt(int64(v.Len())), nil
	case *Object:
		return o.MakeNumberInt(int64(v.Len())), nil
	case fjson.Object:
		return o.MakeNumberInt(int64(v.Len())), nil
	case *Set:
		return o.MakeNumberInt(int64(v.Len())), nil
	case fjson.String:
		return o.MakeNumberInt(int64(len(v.Value()))), nil
	case IterNamespace:
		obj, err := castJSON(ctx, v)
		if err != nil {
			return nil, err
		}

		return o.MakeNumberInt(int64(obj.(fjson.Object).Len())), nil
	default:
		if _, err := castJSON(ctx, v); err != nil {
			return nil, err
		}

		return o.MakeNumberInt(0), nil
	}
}

func (*dataOperations) ObjectGet(ctx context.Context, object, key interface{}) (interface{}, bool, error) {
	jobject, err := castJSON(ctx, object)
	if err != nil {
		return nil, false, err
	}

	jkey, err := castJSON(ctx, key)
	if err != nil {
		return nil, false, err
	}

	switch object := jobject.(type) {
	case *Object:
		value, ok := object.Get(jkey)
		return value, ok, nil

	case fjson.Object:
		s, ok := jkey.(fjson.String)
		if !ok {
			return nil, false, nil
		}

		value := object.Value(s.Value())
		return value, value != nil, nil
	}

	return nil, false, nil
}

func (*dataOperations) ObjectInsert(ctx context.Context, object, key, value interface{}) error {
	if _, ok := object.(IterNamespace); ok {
		return ErrIllegalIter // Can't modify a persisted object.
	}

	jobject, err := castJSON(ctx, object)
	if err != nil {
		return err
	}

	jkey, err := castJSON(ctx, key)
	if err != nil {
		return err
	}

	jvalue, err := castJSON(ctx, value)
	if err != nil {
		return err
	}

	switch object := jobject.(type) {
	case *Object:
		object.Insert(jkey, jvalue)

	case fjson.Object:
		s, ok := jkey.(fjson.String)
		if !ok {
			// Evaluation should never try to modify a JSON it loaded from disk.
			panic("not reached")
		}

		object.Set(s.Value(), jvalue)
	}

	return nil
}

func (o *dataOperations) ObjectMerge(a, b interface{}) (interface{}, error) {
	_, okaa := a.(fjson.Object)
	_, okab := a.(*Object)
	_, okba := b.(fjson.Object)
	_, okbb := b.(*Object)

	if (!okaa && !okab) || (!okba && !okbb) {
		return a, nil
	}

	merged := NewObject()

	var err error

	objectIterate(a, func(key, value fjson.Json) bool {
		other, ok := objectGet(b, key)
		if !ok {
			merged.Insert(key, value)
			return false
		}

		var m interface{}
		m, err = o.ObjectMerge(value, other)
		if err != nil {
			return true
		}

		merged.Insert(key, m.(fjson.Json))
		return false
	})
	if err != nil {
		return nil, err
	}

	objectIterate(b, func(key, value fjson.Json) bool {
		if _, ok := objectGet(a, key); !ok {
			merged.Insert(key, value)
		}
		return false
	})

	return merged, nil
}

func objectIterate(obj interface{}, f func(key, value fjson.Json) bool) {
	if obj, ok := obj.(fjson.Object); ok {
		for i, name := range obj.Names() {
			if f(fjson.NewString(name), obj.Iterate(i)) {
				break
			}
		}

		return
	}

	obj.(*Object).Iter(f)
}

func objectGet(obj interface{}, key interface{}) (fjson.Json, bool) {
	if obj, ok := obj.(fjson.Object); ok {
		if s, ok := key.(fjson.String); ok {
			value := obj.Value(s.Value())
			return value, value != nil
		}

		return nil, false
	}

	return obj.(*Object).Get(key.(fjson.Json))
}

func (*dataOperations) SetAdd(ctx context.Context, set, value interface{}) error {
	jvalue, err := castJSON(ctx, value)
	if err != nil {
		return err
	}

	switch set := set.(type) {
	case *Set:
		set.Add(jvalue)
	default:
		if _, err := castJSON(ctx, set); err != nil {
			return err
		}
	}

	return nil
}

func (o *dataOperations) ToAST(ctx context.Context, v interface{}) (ast.Value, error) {
	switch v := v.(type) {
	case fjson.Null:
		return ast.Null{}, nil

	case fjson.Bool:
		return ast.Boolean(v.Value()), nil

	case fjson.Float:
		return ast.Number(string(v.Value())), nil

	case fjson.String:
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

	case *Object:
		var err error

		terms := make([][2]*ast.Term, 0, v.Len())
		v.Iter(func(k, v fjson.Json) bool {
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

			terms = append(terms, [2]*ast.Term{ast.NewTerm(a), ast.NewTerm(b)})
			return false
		})
		if err != nil {
			return nil, err
		}
		obj := ast.NewObject(terms...)
		return obj, nil

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

	case IterNamespace:
		obj := ast.NewObject()
		var err error
		v.Iter(ctx, func(k, v interface{}) bool {
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
		})
		if err != nil {
			return nil, err
		}
		return obj, nil

	case GetNamespace:
		// No enough of primary key gathered to iterate.
		return nil, ErrIllegalIter
	}

	notImplemented()
	return nil, nil
}

// ToInterface converts the data to golang native presentation.
func (o *dataOperations) ToInterface(ctx context.Context, v interface{}) (interface{}, error) {
	v, err := o.ToAST(ctx, v)
	if err != nil {
		return nil, err
	}

	return ast.JSONWithOpt(v.(ast.Value), ast.JSONOpt{SortSets: false})
}

// Example JSON type library

type Set struct {
	fjson.Json
	set *util.HashMap
}

func NewSet() *Set {
	return &Set{set: util.NewHashMap(equalT, intHash)}
}

func (s *Set) Add(v fjson.Json) {
	s.set.Put(v, fjson.NewNull())
}

func (s *Set) Get(k fjson.Json) (fjson.Json, bool) {
	if _, ok := s.set.Get(k); ok {
		return k, true
	}

	return nil, false
}

func (s *Set) Iter(iter func(v fjson.Json) bool) bool {
	return s.set.Iter(func(v, _ util.T) bool {
		return iter(v.(fjson.Json))
	})
}

func (s *Set) Len() int {
	return s.set.Len()
}

func (s *Set) Equal(y *Set) bool {
	if s == y {
		return true
	}

	if s.Len() != y.Len() {
		return false
	}

	match := true
	s.Iter(func(v fjson.Json) bool {
		_, match = y.Get(v)
		return !match
	})

	return match
}

type Object struct {
	fjson.Json
	obj *util.HashMap
}

func NewObject() *Object {
	return &Object{obj: util.NewHashMap(equalT, intHash)}
}

func (o *Object) Insert(k, v fjson.Json) {
	o.obj.Put(k, v)
}

func (o *Object) Get(k fjson.Json) (fjson.Json, bool) {
	if v, ok := o.obj.Get(k); ok {
		return v.(fjson.Json), true
	}

	return nil, false
}

func (o *Object) Iter(iter func(k, v fjson.Json) bool) bool {
	return o.obj.Iter(func(k, v util.T) bool {
		return iter(k.(fjson.Json), v.(fjson.Json))
	})
}

func (o *Object) Len() int {
	return o.obj.Len()
}

func (o *Object) Equal(other interface{}) bool {
	if other, ok := other.(fjson.Object); ok {
		if o.Len() != other.Len() {
			return false
		}

		eq := true
		o.Iter(func(k, va fjson.Json) bool {
			s, ok := k.(fjson.String)
			if !ok {
				eq = false
				return false
			}

			vb := other.Value(s.Value())
			if vb == nil {
				eq = false
				return false
			}

			eq = equalOp(va, vb)
			return !eq
		})

		return eq
	}

	ob, ok := other.(*Object)
	if !ok {
		return false
	}

	if o == ob {
		return true
	}

	if o.Len() != ob.Len() {
		return false
	}

	eq := true
	o.Iter(func(k, a fjson.Json) bool {
		b, ok := ob.Get(k)
		if !ok {
			eq = false
			return false
		}

		eq = equalOp(a, b)
		return !eq
	})

	return eq
}

func castJSON(ctx context.Context, v interface{}) (fjson.Json, error) {
	j, ok := v.(fjson.Json)
	if ok {
		return j, nil
	}

	switch v := v.(type) {
	case IterNamespace:
		obj := fjson.NewObject(nil)
		var err error
		v.Iter(ctx, func(k, v interface{}) bool {
			obj.Set(k.(fjson.String).Value(), v.(fjson.Json))
			return false
		})
		if err != nil {
			return nil, err
		}
		return obj, nil

	case GetNamespace:
		// No enough of primary key gathered to iterate.
		return nil, ErrIllegalIter
	}

	return nil, ErrInvalidData
}

func equalT(a, b util.T) bool {
	return equalOp(a.(fjson.Json), b.(fjson.Json))
}

func equalOp(a, b fjson.Json) bool {
	switch x := a.(type) {
	case fjson.Null:
		_, ok := b.(fjson.Null)
		return ok

	case fjson.Bool:
		if y, ok := b.(fjson.Bool); ok {
			return x.Value() == y.Value()
		}

		return false

	case fjson.Float:
		if y, ok := b.(fjson.Float); ok {
			return compare(x, y) == 0
		}

		return false

	case fjson.String:
		if y, ok := b.(fjson.String); ok {
			return x.Value() == y.Value()
		}

		return false

	case fjson.Array:
		if y, ok := b.(fjson.Array); ok {
			if x.Len() != y.Len() {
				return false
			}

			for i := 0; i < x.Len(); i++ {
				if !equalOp(x.Iterate(i), y.Iterate(i)) {
					return false
				}
			}

			return true
		}

		return false

	case *Object:
		return x.Equal(b)

	case fjson.Object:
		if y, ok := b.(fjson.Object); ok {
			return x.Compare(y) == 0
		}

		if y, ok := b.(*Object); ok {
			return y.Equal(x)
		}

		return false

	case *Set:
		if y, ok := b.(*Set); ok {
			return x.Equal(y)
		}

		return false

	default:
		panic("unsupported type")
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

func notImplemented() {
	panic("not implemented")
}

// Borrowed definitions from OPA's ast/term to allow using Golang's default sort on term slices.
type termSlice []*ast.Term

func (s termSlice) Less(i, j int) bool { return ast.Compare(s[i].Value, s[j].Value) < 0 }
func (s termSlice) Swap(i, j int)      { x := s[i]; s[i] = s[j]; s[j] = x }
func (s termSlice) Len() int           { return len(s) }
