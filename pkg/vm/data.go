package vm

import (
	"context"
	"encoding/base64"
	gojson "encoding/json"
	"errors"
	"strconv"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"

	"github.com/open-policy-agent/opa/ast"
	"golang.org/x/exp/slices"
)

var (
	ErrInvalidData = errors.New("invalid data")      // Unsupported data type detected.
	ErrIllegalIter = errors.New("illegal iteration") // Illegal iteration over persisted table detected.

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
		Iter(ctx context.Context, f func(key, value interface{}) (bool, error)) error
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

	case Set:
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
		return value, nil
	case fjson.Bool:
		return value, nil
	case fjson.Float:
		return value, nil
	case *fjson.String:
		return value, nil
	case fjson.Array:
		return v.Clone(false), nil
	case fjson.Object:
		return v.Clone(false), nil
	case Set:
		set := NewSet()
		_, err := v.Iter(func(v fjson.Json) (bool, error) {
			var err error
			set, err = set.Add(ctx, v)
			return err != nil, err
		})
		return set, err
	case IterableObject:
		// TODO: Return a copy-on-write object.
		obj := NewObject()
		if err := v.Iter(ctx, func(k, v interface{}) (bool, error) {
			var err error
			obj, err = obj.Insert(ctx, k, v)
			return err != nil, err
		}); err != nil {
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
		return o.MakeNull(), nil
	case ast.Null:
		return o.MakeNull(), nil
	case bool:
		return o.MakeBoolean(x), nil
	case ast.Boolean:
		return o.MakeBoolean(bool(x)), nil
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
		return fjson.NewArray(values, len(x)), nil
	case []interface{}:
		values := make([]fjson.File, 0, len(x))
		for _, v := range x {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return nil, err
			}
			values = append(values, y.(fjson.Json))
		}
		return fjson.NewArray(values, len(x)), nil
	case *ast.Array:
		n := x.Len()
		arr := fjson.NewArray(make([]fjson.File, n), n)
		for i := 0; i < n; i++ {
			y, err := o.FromInterface(ctx, x.Elem(i).Value)
			if err != nil {
				return nil, err
			}
			arr.SetIdx(i, y.(fjson.File))
		}
		return arr, nil
	case map[string]interface{}:
		obj := NewObject()
		for k, v := range x {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return nil, err
			}
			if obj, err = obj.Insert(ctx, fjson.NewString(k), y.(fjson.Json)); err != nil {
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
		return fjson.NewArray(values, len(x)), nil
	case map[string]string:
		obj := NewObject()
		for k, v := range x {
			var err error
			if obj, err = obj.Insert(ctx, fjson.NewString(k), fjson.NewString(v)); err != nil {
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
			obj, err = obj.Insert(ctx, kj.(fjson.Json), vj.(fjson.Json))
			return err
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
			set, err = set.Add(ctx, y.(fjson.Json))
			return err
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
		y, err := toNative(x)
		if err != nil {
			notImplemented()
		}
		return o.FromInterface(ctx, y)
	}

	return nil, nil
}

func (o *DataOperations) Iter(ctx context.Context, v interface{}, f func(key, value interface{}) (bool, error)) error {
	switch v := v.(type) {
	case fjson.Array:
		n := v.Len()
		for i := 0; i < n; i++ {
			if stop, err := f(o.MakeNumberInt(int64(i)), v.Iterate(i)); err != nil {
				return err
			} else if stop {
				break
			}
		}
	case fjson.Object:
		for i, key := range v.Names() {
			if stop, err := f(fjson.NewString(key), v.Iterate(i)); err != nil {
				return err
			} else if stop {
				break
			}
		}
	case Set:
		_, err := v.Iter(func(v fjson.Json) (bool, error) {
			stop, err := f(v, v)
			if err != nil {
				return true, err
			}

			return stop, nil
		})
		return err

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

func (*DataOperations) MakeArray(capacity int32) fjson.Array {
	return fjson.NewArray(nil, int(capacity))
}

func (*DataOperations) MakeBoolean(v bool) fjson.Json {
	if v {
		return prebuiltTrue
	}

	return prebuiltFalse
}

func (*DataOperations) MakeObject() Object {
	// TODO: Planner emits code that breaks with generic rule
	// heads, unless the objects constructed allow in-place
	// key-value insertion. Compact object types don't allow that:
	// once they reach their capacity limit, a new (bigger)
	// instance has to be created.
	return newObjectLarge(0)
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

func (*DataOperations) MakeSet() Set {
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
	case Set:
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
	case Object:
		o, err = o.Insert(ctx, jkey, value)
		return o, o != object, err

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

	if err := objectIterate(ctx, a, func(key, value interface{}) (bool, error) {
		other, ok, err := objectGet(ctx, b, key)
		if err != nil {
			return true, err
		} else if !ok {
			merged, err = merged.Insert(ctx, key, value)
			return err != nil, err
		}

		m, err := o.ObjectMerge(ctx, value, other)
		if err != nil {
			return true, err
		}

		merged, err = merged.Insert(ctx, key, m)
		return err != nil, err
	}); err != nil {
		return nil, err
	}

	if err := objectIterate(ctx, b, func(key, value interface{}) (bool, error) {
		_, ok, err := objectGet(ctx, a, key)
		if err != nil {
			return true, err
		}
		if !ok {
			merged, err = merged.Insert(ctx, key, value)
			if err != nil {
				return true, err
			}
		}
		return false, nil
	}); err != nil {
		return nil, err
	}

	return merged, nil
}

func objectIterate(ctx context.Context, obj interface{}, f func(key, value interface{}) (bool, error)) error {
	if obj, ok := obj.(fjson.Object); ok {
		for i, name := range obj.Names() {
			if stop, err := f(fjson.NewString(name), obj.Iterate(i)); err != nil {
				return err
			} else if stop {
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

func (*DataOperations) SetAdd(ctx context.Context, set, value interface{}) (interface{}, error) {
	jvalue, err := castJSON(ctx, value)
	if err != nil {
		return nil, err
	}

	switch set := set.(type) {
	case Set:
		return set.Add(ctx, jvalue)
	default:
		if _, err := castJSON(ctx, set); err != nil {
			return nil, err
		}
	}

	return set, nil
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

	case Set:
		// TODO: Sorting is for deterministic tests.
		// We prealloc the Term array and sort it here to trim down the total number of allocs.
		terms := make([]*ast.Term, 0, v.Len())
		_, err := v.Iter(func(v fjson.Json) (bool, error) {
			a, err := o.ToAST(ctx, v)
			if err != nil {
				return true, err
			}

			terms = append(terms, ast.NewTerm(a))
			return false, nil
		})
		if err != nil {
			return nil, err
		}

		slices.SortFunc(terms, func(a, b *ast.Term) int {
			return a.Value.Compare(b.Value)
		})
		set := ast.NewSet(terms...)
		return set, nil

	case IterableObject:
		obj := ast.NewObject()
		if err := v.Iter(ctx, func(k, v interface{}) (bool, error) {
			a, err := o.ToAST(ctx, k)
			if err != nil {
				return true, err
			}

			b, err := o.ToAST(ctx, v)
			if err != nil {
				return true, err
			}

			obj.Insert(ast.NewTerm(a), ast.NewTerm(b))
			return false, nil
		}); err != nil {
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

func castJSON(ctx context.Context, v interface{}) (fjson.Json, error) {
	j, ok := v.(fjson.Json)
	if ok {
		return j, nil
	}

	switch v := v.(type) {
	case IterableObject:
		obj := NewObject()
		err := v.Iter(ctx, func(k, v interface{}) (bool, error) {
			var err error
			obj, err = obj.Insert(ctx, k.(*fjson.String), v.(fjson.Json))
			return false, err
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
