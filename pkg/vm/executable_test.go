package vm

import (
	"reflect"
	"testing"
)

func test(t testing.TB) {
	// Executable

	{
		s, f, p := []byte("strings"), []byte("functions"), []byte("plans")
		e := Executable(Executable{}.Write(s, f, p))

		check(t, "valid", e.IsValid(), true)
	}

	// Header

	{
		version, totalLength, stringsOffset, functionsOffset, plansOffset := uint32(1), uint32(headerLength), uint32(0), uint32(0), uint32(0)
		h := header(header{}.Write(version, totalLength, stringsOffset, functionsOffset, plansOffset))

		check(t, "valid", h.IsValid(), true)
	}

	// Strings

	{
		s := []string{"abc", "def"}
		strings{}.Write(s)
	}

	// builtin

	{
		b := builtin(builtin{}.Write("builtin", false))

		check(t, "name", b.Name(), "builtin")
		check(t, "relation", b.Relation(), false)
		check(t, "size", size(b), int(len(b)))
	}

	// Plans

	{
		d := [][]byte{[]byte("plan1"), []byte("plan2")}
		p := plans(plans{}.Write(d))

		check(t, "len", p.Len(), 2)
	}

	// Plan

	{
		d := []byte("plan data")
		p := plan(plan{}.Write("plan1", d))

		check(t, "name", p.Name(), "plan1")
	}

	// Blocks

	{
		d := [][]byte{[]byte("block1"), []byte("block2")}
		b := blocks(blocks{}.Write(d))

		check(t, "size", size(b), len(b))
		check(t, "len", b.Len(), 2)
	}

	// Block

	{
		d := [][]byte{[]byte("statement1"), []byte("statement2")}
		b := block(block{}.Write(d))

		check(t, "size", size(b), int(len(b)))
	}

	// Function

	{
		// func (function) Write(name string, index int, params []Local, ret Local, blocks []byte, path []string) []byte
		name, index, params, ret, blocks, path := "name", 1, []Local{Local(2)}, Local(3), blocks("blocks"), []string{"path1", "path2"}
		f := function(function{}.Write(name, index, params, ret, blocks, path))

		check(t, "size", size(f), len(f))
		check(t, "name", f.Name(), name)
		check(t, "index", f.Index(), index)
		check(t, "params", f.Params(), params)
		check(t, "return", f.Return(), ret)
		check(t, "blocks", f.Blocks(), blocks)
		check(t, "path", f.Path(), path)
		check(t, "builtin", f.IsBuiltin(), false)
		checkFunctionParams(t, f)
		checkFunctionPath(t, f)
	}

	// ArrayAppend

	{
		value, array := NewStringIndexConst(1), Local(2)
		s := arrayAppend(arrayAppend{}.Write(value, array))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementArrayAppend))
		check(t, "value", s.Value(), value)
		check(t, "array", s.Array(), array)
	}

	// AssignInt

	{
		value, target := int64(1), Local(2)
		s := assignInt(assignInt{}.Write(value, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementAssignInt))
		check(t, "value", s.Value(), value)
		check(t, "target", s.Target(), target)
	}

	// AssignVar

	{
		ssource, target := NewStringIndexConst(1), Local(2)
		s := assignVar(assignVar{}.Write(ssource, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementAssignVar))
		check(t, "source", s.Source(), ssource)
		check(t, "target", s.Target(), target)

		bsource, target := NewBoolConst(true), Local(2)
		s = assignVar(assignVar{}.Write(bsource, target))

		check(t, "source", s.Source(), bsource)

		lsource, target := NewLocal(1), Local(2)
		s = assignVar(assignVar{}.Write(lsource, target))

		check(t, "source", s.Source(), lsource)
	}

	// AssignVarOnce

	{
		source, target := NewStringIndexConst(1), Local(2)
		s := assignVarOnce(assignVarOnce{}.Write(source, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementAssignVarOnce))
		check(t, "source", s.Source(), source)
		check(t, "target", s.Target(), target)
	}

	// BlockStmt

	{
		blocks := blocks("block")
		s := blockStmt(blockStmt{}.Write(blocks))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementBlockStmt))
		check(t, "block", s.Blocks(), blocks)
	}

	// BreakStmt

	{
		index := uint32(1)
		s := breakStmt(breakStmt{}.Write(index))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementBreakStmt))
		check(t, "index", s.Index(), index)
	}

	// CallDynamic

	{
		args, result, path := []Local{Local(0), Local(1)}, Local(2), []LocalOrConst{NewLocal(3), NewStringIndexConst(1), NewBoolConst(false)}
		s := callDynamic(callDynamic{}.Write(args, result, path))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementCallDynamic))
		check(t, "args", s.Args(), args)
		check(t, "result", s.Result(), result)
		check(t, "path", s.Path(), path)
		checkCallDynamicPath(t, s)
		checkCallDynamicArgs(t, s)
	}

	// Call

	{
		index, args, result := 1, []LocalOrConst{NewLocal(1), NewLocal(2), NewStringIndexConst(1), NewBoolConst(false)}, Local(3)
		s := call(call{}.Write(index, args, result))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementCall))
		check(t, "func", s.Func(), index)
		check(t, "args", s.Args(), args)
		check(t, "result", s.Result(), result)
		checkCallArgs(t, s)
	}

	// Dot

	{
		source, key, target := NewStringIndexConst(1), NewStringIndexConst(2), Local(3)
		s := dot(dot{}.Write(source, key, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementDot))
		check(t, "source", s.Source(), source)
		check(t, "key", s.Key(), key)
		check(t, "target", s.Target(), target)
	}

	// Equal

	{
		a, b := NewStringIndexConst(1), NewStringIndexConst(2)
		s := equal(equal{}.Write(a, b))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementEqual))
		check(t, "a", s.A(), a)
		check(t, "b", s.B(), b)
	}

	// IsArray

	{
		source := NewLocal(1)
		s := isArray(isArray{}.Write(source))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementIsArray))
		check(t, "source", s.Source(), source)
	}

	// IsDefined

	{
		source := Local(1)
		s := isDefined(isDefined{}.Write(source))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementIsDefined))
		check(t, "source", s.Source(), source)
	}

	// IsObject

	{
		source := NewLocal(1)
		s := isObject(isObject{}.Write(source))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementIsObject))
		check(t, "source", s.Source(), source)
	}

	// IsUndefined

	{
		source := Local(1)
		s := isUndefined(isUndefined{}.Write(source))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementIsUndefined))
		check(t, "source", s.Source(), source)
	}

	// Len

	{
		source, target := NewStringIndexConst(1), Local(2)
		s := lenStmt(lenStmt{}.Write(source, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementLen))
		check(t, "source", s.Source(), source)
		check(t, "target", s.Target(), target)
	}

	// MakeArray

	{
		capacity, target := int32(1), Local(2)
		s := makeArray(makeArray{}.Write(capacity, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementMakeArray))
		check(t, "capacity", s.Capacity(), capacity)
		check(t, "target", s.Target(), target)
	}

	// MakeNull

	{
		target := Local(1)
		s := makeNull(makeNull{}.Write(target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementMakeNull))
		check(t, "target", s.Target(), target)
	}

	// MakeNumberInt

	{
		value, target := int64(1), Local(2)
		s := makeNumberInt(makeNumberInt{}.Write(value, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementMakeNumberInt))
		check(t, "value", s.Value(), value)
		check(t, "target", s.Target(), target)
	}

	// MakeNumberRef

	{
		index, target := int(1), Local(2)
		s := makeNumberRef(makeNumberRef{}.Write(index, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementMakeNumberRef))
		check(t, "index", s.Index(), index)
		check(t, "target", s.Target(), target)
	}

	// MakeObject

	{
		target := Local(1)
		s := makeObject(makeObject{}.Write(target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementMakeObject))
		check(t, "target", s.Target(), target)
	}

	// MakeSet

	{
		target := Local(1)
		s := makeSet(makeSet{}.Write(target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementMakeSet))
		check(t, "target", s.Target(), target)
	}

	// Nop

	{
		s := nop(nop{}.Write())

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementNop))
	}

	// Not

	{
		block := block("block")
		s := not(not{}.Write(block))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementNot))
		check(t, "block", s.Block(), block)
	}

	// NotEqual

	{
		a, b := NewStringIndexConst(1), NewStringIndexConst(2)
		s := notEqual(notEqual{}.Write(a, b))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementNotEqual))
		check(t, "a", s.A(), a)
		check(t, "b", s.B(), b)
	}

	// ObjectInsertOnce

	{
		key, value, object := NewStringIndexConst(1), NewStringIndexConst(2), Local(3)
		s := objectInsertOnce(objectInsertOnce{}.Write(key, value, object))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementObjectInsertOnce))
		check(t, "key", s.Key(), key)
		check(t, "value", s.Value(), value)
		check(t, "object", s.Object(), object)
	}

	// ObjectInsert

	{
		key, value, object := NewStringIndexConst(1), NewStringIndexConst(2), Local(3)
		s := objectInsert(objectInsert{}.Write(key, value, object))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementObjectInsert))
		check(t, "key", s.Key(), key)
		check(t, "value", s.Value(), value)
		check(t, "object", s.Object(), object)
	}

	// ObjectMerge

	{
		a, b, target := Local(1), Local(2), Local(3)
		s := objectMerge(objectMerge{}.Write(a, b, target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementObjectMerge))
		check(t, "a", s.A(), a)
		check(t, "b", s.B(), b)
		check(t, "target", s.Target(), target)
	}

	// ResetLocal

	{
		target := Local(1)
		s := resetLocal(resetLocal{}.Write(target))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementResetLocal))
		check(t, "target", s.Target(), target)
	}

	// ResultSetAdd

	{
		value := Local(1)
		s := resultSetAdd(resultSetAdd{}.Write(value))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementResultSetAdd))
		check(t, "value", s.Value(), value)
	}

	// ReturnLocal

	{
		source := Local(1)
		s := returnLocal(returnLocal{}.Write(source))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementReturnLocal))
		check(t, "source", s.Source(), source)
	}

	// Scan

	{
		source, key, value, block := Local(1), Local(2), Local(3), block("block")
		s := scan(scan{}.Write(source, key, value, block))

		check(t, "length", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementScan))
		check(t, "source", s.Source(), source)
		check(t, "key", s.Key(), key)
		check(t, "value", s.Value(), value)
		check(t, "block", s.Block(), block)
	}

	// SetAdd

	{
		value, set := NewStringIndexConst(1), Local(2)
		s := setAdd(setAdd{}.Write(value, set))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementSetAdd))
		check(t, "value", s.Value(), value)
		check(t, "set", s.Set(), set)
	}

	// With

	{
		local, path, value, block := Local(1), []int{0, 1}, NewStringIndexConst(2), block("block")
		s := with(with{}.Write(local, path, value, block))

		check(t, "size", size(s), len(s))
		check(t, "type", s.Type(), uint32(typeStatementWith))
		check(t, "local", s.Local(), local)
		check(t, "path", s.Path(), path)
		check(t, "value", s.Value(), value)
		check(t, "block", s.Block(), block)
		checkWithPath(t, s)
	}
}

func TestExecutable(t *testing.T) {
	test(t)
}

func BenchmarkExecutable(b *testing.B) {
	for i := 0; i < b.N; i++ {
		test(b)
	}
}

func check(t testing.TB, field string, a, b interface{}) {
	if !reflect.DeepEqual(a, b) {
		t.Errorf("field not equal %v: %v %v", field, a, b)
	}
}

func checkFunctionParams(t testing.TB, a function) {
	p := a.Params()

	if uint32(len(p)) != a.ParamsLen() {
		t.Fatalf("invalid params len: %d, %d", len(p), a.ParamsLen())
	}

	checkIter(t, p, a.ParamsIter)
}

func checkFunctionPath(t testing.TB, a function) {
	p := a.Path()

	if uint32(len(p)) != a.PathLen() {
		t.Fatalf("invalid params len: %d, %d", len(p), a.PathLen())
	}

	checkIter(t, p, a.PathIter)
}

func checkCallDynamicPath(t testing.TB, a callDynamic) {
	p := a.Path()

	if uint32(len(p)) != a.PathLen() {
		t.Fatalf("invalid params len: %d, %d", len(p), a.PathLen())
	}

	checkLocalOrConstIter(t, p, a.PathIter)
}

func checkWithPath(t testing.TB, a with) {
	p := a.Path()

	if uint32(len(p)) != a.PathLen() {
		t.Fatalf("invalid params len: %d, %d", len(p), a.PathLen())
	}

	checkIter(t, p, a.PathIter)
}

func checkCallDynamicArgs(t testing.TB, a callDynamic) {
	p := a.Args()

	if uint32(len(p)) != a.ArgsLen() {
		t.Fatalf("invalid params len: %d, %d", len(p), a.ArgsLen())
	}

	checkIter(t, p, a.ArgsIter)
}

func checkCallArgs(t testing.TB, a call) {
	p := a.Args()

	if uint32(len(p)) != a.ArgsLen() {
		t.Fatalf("invalid params len: %d, %d", len(p), a.ArgsLen())
	}

	checkLocalOrConstIter(t, p, a.ArgsIter)
}

func checkIter[T Local | string | int](t testing.TB, p []T, iter func(fcn func(i uint32, v T) error) error) {
	iter(func(i uint32, v T) error {
		if p[i] != v {
			t.Fatalf("invalid params %v, %v", p[i], v)
		}
		return nil
	})
}

func checkLocalOrConstIter(t testing.TB, p []LocalOrConst, iter func(fcn func(i uint32, v LocalOrConst) error) error) {
	iter(func(i uint32, v LocalOrConst) error {
		if p[i] != v {
			t.Fatalf("invalid params %v, %v", p[i], v)
		}
		return nil
	})
}

func size(data []byte) int {
	return int(getLen(data))
}
