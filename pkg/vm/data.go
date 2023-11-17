package vm

import (
	"context"
	"encoding/base64"
	gojson "encoding/json"
	"errors"
	"strconv"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"

	"github.com/open-policy-agent/opa/ast"
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

	case fjson.Set:
		value, ok := v.Get(jkey)
		return value, ok, nil

	case fjson.Object2:
		value, ok := v.Get(jkey)
		return value, ok, nil

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
	case fjson.Set:
		set := fjson.NewSet(v.Len())
		_, err := v.Iter(func(v fjson.Json) (bool, error) {
			set = set.Add(v)
			return false, nil
		})
		return set, err
	case fjson.Object2:
		// TODO: Return a copy-on-write object.
		obj := fjson.NewObject2(v.Len())
		if err := v.Iter(func(k, v fjson.Json) (bool, error) {
			obj = obj.Insert(k, v)
			return false, nil
		}); err != nil {
			return nil, err
		}

		return obj, nil

	case IterableObject:
		// TODO: Return a copy-on-write object.
		obj := fjson.NewObject2(0)
		if err := v.Iter(ctx, func(k, v interface{}) (bool, error) {
			kj, err := castJSON(ctx, k)
			if err != nil {
				return true, err
			}

			vj, err := castJSON(ctx, v)
			if err != nil {
				return true, err
			}

			obj = obj.Insert(kj, vj)
			return false, nil
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
	ja, err := castJSON(ctx, a)
	if err != nil {
		return false, err
	}

	jb, err := castJSON(ctx, b)
	if err != nil {
		return false, err
	}

	return fjson.Equal(ja, jb), nil
}

// FromInterface converts a golang native data to internal representation.
func (o *DataOperations) FromInterface(ctx context.Context, x interface{}) (fjson.Json, error) {
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
			values = append(values, y)
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
		obj := fjson.NewObject2(len(x))
		for k, v := range x {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return nil, err
			}
			obj = obj.Insert(fjson.NewString(k), y)
		}
		return obj, nil
	case []map[string]interface{}:
		values := make([]fjson.File, 0, len(x))
		for _, v := range x {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return nil, err
			}
			values = append(values, y)
		}
		return fjson.NewArray(values, len(x)), nil
	case map[string]string:
		obj := fjson.NewObject2(len(x))
		for k, v := range x {
			obj = obj.Insert(fjson.NewString(k), fjson.NewString(v))
		}
		return obj, nil
	case ast.Object:
		obj := fjson.NewObject2(x.Len())
		if err := x.Iter(func(k, v *ast.Term) error {
			kj, err := o.FromInterface(ctx, k)
			if err != nil {
				return err
			}
			vj, err := o.FromInterface(ctx, v)
			if err != nil {
				return err
			}
			obj = obj.Insert(kj, vj)
			return nil
		}); err != nil {
			return nil, err
		}
		return obj, nil
	case ast.Ref:
		notImplemented()
	case ast.Set:
		set := fjson.NewSet(x.Len())
		err := x.Iter(func(v *ast.Term) error {
			y, err := o.FromInterface(ctx, v)
			if err != nil {
				return err
			}
			set = set.Add(y)
			return nil
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
	case fjson.Set:
		_, err := v.Iter2(f)
		return err

	case fjson.Object2:
		return v.Iter2(f)

	case IterableObject:
		return v.Iter(ctx, f)
	}

	return nil
}

func (*DataOperations) IsArray(ctx context.Context, v interface{}) (bool, error) {
	switch v.(type) {
	case fjson.Array:
		return true, nil
	case IterableObject, fjson.Object, fjson.Object2:
		return false, nil
	default:
		_, err := castJSON(ctx, v) // TODO: Remove
		return false, err
	}
}

func (*DataOperations) IsObject(ctx context.Context, v interface{}) (bool, error) {
	switch v.(type) {
	case fjson.Object, IterableObject, fjson.Object2:
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

func (*DataOperations) MakeObject() fjson.Object2 {
	return fjson.NewObject2(0)
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

func (*DataOperations) MakeSet() fjson.Set {
	return fjson.NewSet(0)
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
	case fjson.Set:
		return o.MakeNumberInt(int64(v.Len())), nil
	case *fjson.String:
		return o.MakeNumberInt(int64(len(v.Value()))), nil
	case fjson.Object2:
		return o.MakeNumberInt(int64(v.Len())), nil
	case IterableObject:
		var n int64
		if err := v.Iter(ctx, func(_ any, _ any) (bool, error) {
			n++
			return false, nil
		}); err != nil {
			return nil, err
		}

		return o.MakeNumberInt(n), nil

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

	case fjson.Object2:
		value, ok := object.Get(jkey)
		return value, ok, nil

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
	case fjson.Object2:
		jvalue, err := castJSON(ctx, value)
		if err != nil {
			return nil, false, err
		}

		o = o.Insert(jkey, jvalue)
		return o, o != object, nil

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
	_, okac := a.(fjson.Object2)
	_, okba := b.(fjson.Object)
	_, okbb := b.(IterableObject)
	_, okbc := b.(fjson.Object2)

	if (!okaa && !okab && !okac) || (!okba && !okbb && !okbc) {
		return a, nil
	}

	merged := fjson.NewObject2(0)

	if err := objectIterate(ctx, a, func(key, value fjson.Json) (bool, error) {
		other, ok, err := objectGet(ctx, b, key)
		if err != nil {
			return true, err
		} else if !ok {
			merged = merged.Insert(key, value)
			return false, nil
		}

		m, err := o.ObjectMerge(ctx, value, other)
		if err != nil {
			return true, err
		}

		mj, err := castJSON(ctx, m)
		if err != nil {
			return true, err
		}

		merged = merged.Insert(key, mj)
		return false, nil
	}); err != nil {
		return nil, err
	}

	if err := objectIterate(ctx, b, func(key, value fjson.Json) (bool, error) {
		_, ok, err := objectGet(ctx, a, key)
		if err != nil {
			return true, err
		}
		if !ok {
			merged = merged.Insert(key, value)
		}
		return false, nil
	}); err != nil {
		return nil, err
	}

	return merged, nil
}

func objectIterate(ctx context.Context, obj interface{}, f func(key, value fjson.Json) (bool, error)) error {
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

	if obj, ok := obj.(fjson.Object2); ok {
		return obj.Iter(f)
	}

	return obj.(IterableObject).Iter(ctx, func(key, value interface{}) (bool, error) {
		jkey, err := castJSON(ctx, value)
		if err != nil {
			return false, err
		}

		jvalue, err := castJSON(ctx, value)
		if err != nil {
			return false, err
		}

		return f(jkey, jvalue)
	})
}

func objectGet(ctx context.Context, obj interface{}, key fjson.Json) (interface{}, bool, error) {
	if obj, ok := obj.(fjson.Object); ok {
		if s, ok := key.(*fjson.String); ok {
			value := obj.Value(s.Value())
			return value, value != nil, nil
		}

		return nil, false, nil
	}

	if obj, ok := obj.(fjson.Object2); ok {
		value, ok := obj.Get(key)
		return value, ok, nil
	}

	return obj.(IterableObject).Get(ctx, key)
}

func (*DataOperations) SetAdd(ctx context.Context, set, value interface{}) (interface{}, error) {
	jvalue, err := castJSON(ctx, value)
	if err != nil {
		return nil, err
	}

	switch set := set.(type) {
	case fjson.Set:
		return set.Add(jvalue), nil
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
		return v.AST(), nil

	case fjson.Bool:
		return v.AST(), nil

	case fjson.Float:
		return v.AST(), nil

	case *fjson.String:
		return v.AST(), nil

	case fjson.Array:
		return v.AST(), nil

	case fjson.Object:
		return v.AST(), nil

	case fjson.Set:
		return v.AST(), nil

	case fjson.Object2:
		return v.AST(), nil

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
		obj := fjson.NewObject2(0)
		err := v.Iter(ctx, func(k, v interface{}) (bool, error) {
			jk, err := castJSON(ctx, k)
			if err != nil {
				return false, err
			}

			jv, err := castJSON(ctx, v)
			if err != nil {
				return false, err
			}

			obj = obj.Insert(jk, jv)
			return false, nil
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
