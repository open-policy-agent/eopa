package vm

import (
	"bytes"
	"encoding/binary"
)

const (
	typeStatementArrayAppend      = 1
	typeStatementAssignInt        = 2
	typeStatementAssignVar        = 3
	typeStatementAssignVarOnce    = 4
	typeStatementBlockStmt        = 5
	typeStatementBreakStmt        = 6
	typeStatementCall             = 7
	typeStatementCallDynamic      = 8
	typeStatementDot              = 9
	typeStatementEqual            = 10
	typeStatementIsArray          = 11
	typeStatementIsDefined        = 12
	typeStatementIsObject         = 13
	typeStatementIsUndefined      = 14
	typeStatementLen              = 15
	typeStatementMakeArray        = 16
	typeStatementMakeNull         = 17
	typeStatementMakeNumberInt    = 18
	typeStatementMakeNumberRef    = 19
	typeStatementMakeObject       = 20
	typeStatementMakeSet          = 21
	typeStatementNop              = 22
	typeStatementNot              = 23
	typeStatementNotEqual         = 24
	typeStatementObjectInsert     = 25
	typeStatementObjectInsertOnce = 26
	typeStatementObjectMerge      = 27
	typeStatementResetLocal       = 28
	typeStatementResultSetAdd     = 29
	typeStatementReturnLocal      = 30
	typeStatementScan             = 31
	typeStatementSetAdd           = 32
	typeStatementWith             = 33

	typeBuiltin  = 0
	typeFunction = 1
)

type (
	Executable []byte

	header []byte

	// Structure

	plans      []byte
	plan       []byte
	strings    []byte
	functions  []byte
	function   []byte
	builtin    []byte
	blocks     []byte
	block      []byte
	statements []byte
	statement  []byte

	// Statements

	arrayAppend      []byte
	assignInt        []byte
	assignVar        []byte
	assignVarOnce    []byte
	blockStmt        []byte
	breakStmt        []byte
	callDynamic      []byte
	call             []byte
	dot              []byte
	equal            []byte
	isArray          []byte
	isDefined        []byte
	isObject         []byte
	isUndefined      []byte
	lenStmt          []byte
	makeArray        []byte
	makeNull         []byte
	makeNumberInt    []byte
	makeNumberRef    []byte
	makeObject       []byte
	makeSet          []byte
	nop              []byte
	not              []byte
	notEqual         []byte
	objectInsert     []byte
	objectInsertOnce []byte
	objectMerge      []byte
	resetLocal       []byte
	resultSetAdd     []byte
	returnLocal      []byte
	scan             []byte
	setAdd           []byte
	with             []byte
)

const (
	magic                       = "rego"
	version                     = 0
	headerMagicOffset           = 0
	headerVersionOffset         = headerMagicOffset + 4
	headerLengthOffset          = headerVersionOffset + 4
	headerStringsOffsetOffset   = headerLengthOffset + 4
	headerFunctionsOffsetOffset = headerStringsOffsetOffset + 4
	headerPlansOffsetOffset     = headerFunctionsOffsetOffset + 4
	headerLength                = headerPlansOffsetOffset + 4
)

func (h header) Write(version uint32, totalLength uint32, stringsOffset uint32, functionsOffset uint32, plansOffset uint32) []byte {
	return concat(
		[]byte(magic),
		newUint32(version),
		newUint32(totalLength),
		newUint32(stringsOffset),
		newUint32(functionsOffset),
		newUint32(plansOffset),
	)
}

func (h header) IsValid() bool {
	if len(h) < headerLength {
		return false
	}

	if !bytes.Equal(h[headerMagicOffset:headerMagicOffset+4], []byte(magic)) {
		return false
	}

	return true
}

func (h header) Version() uint32 {
	return getUint32(h, headerVersionOffset)
}

func (h header) Length() uint32 {
	return getUint32(h, headerLengthOffset)
}

func (h header) StringsOffset() uint32 {
	return getUint32(h, headerStringsOffsetOffset)
}

func (h header) FunctionsOffset() uint32 {
	return getUint32(h, headerFunctionsOffsetOffset)
}

func (h header) PlansOffset() uint32 {
	return getUint32(h, headerPlansOffsetOffset)
}

func (Executable) Write(strings []byte, functions []byte, plans []byte) []byte {
	binary := make([]byte, 0, len(strings)+len(functions)+len(plans))

	stringsOffset := uint32(len(binary))
	binary = append(binary, strings...)

	functionsOffset := uint32(len(binary))
	binary = append(binary, functions...)

	plansOffset := uint32(len(binary))
	binary = append(binary, plans...)

	return append(header{}.Write(version, headerLength+uint32(len(binary)), stringsOffset, functionsOffset, plansOffset), binary...)
}

func (e Executable) IsValid() bool {
	h := header(e)
	if !h.IsValid() {
		return false
	}

	if h.Version() != version {
		return false
	}

	if h.Length() < uint32(len(e)) {
		return false
	}

	return true
}

func (e Executable) Strings() strings {
	stringsOffset := header(e).StringsOffset()
	return strings(e[headerLength+stringsOffset:])
}

func (e Executable) Functions() functions {
	offset := header(e).FunctionsOffset()
	return functions(e[headerLength+offset:])
}

func (e Executable) Plans() plans {
	offset := header(e).PlansOffset()
	return plans(e[headerLength+offset:])
}

func (s strings) Write(strings []string) []byte {
	n := len(strings)
	offsets := uint32(4)
	b := concat(
		newUint32(uint32(n)),
		newOffsetIndex(n),
	)

	for i, s := range strings {
		putOffsetIndex(b, offsets, i, uint32(len(b)))
		b = append(b, newString(s)...)
	}

	return b
}

func (s strings) String(ops DataOperations, i StringIndexConst) Value {
	if n := getUint32(s, 0); uint32(i) >= n {
		panic("corrupted binary")
	}

	stringOffset := getOffsetIndex(s, 4, int(i))
	return ops.MakeString(getString(s, stringOffset))
}

func (f functions) Len() int {
	return int(getUint32(f, 0))
}

func (f functions) Function(i int) function {
	offset := getOffsetIndex(f, 4, i)
	l := getUint32(f, offset)

	return function(f[offset : offset+l])
}

func (function) Write(name string, index int, params []Local, ret Local, blocks []byte, path []string) []byte {
	lengthOffset := uint32(0)
	offsets := uint32(8)
	b := concat(
		newUint32(0), // Length
		newUint32(typeFunction),
		newOffsetIndex(6),
	)

	putOffsetIndex(b, offsets, 0, uint32(len(b)))
	b = append(b, newString(name)...)

	putOffsetIndex(b, offsets, 1, uint32(len(b)))
	b = append(b, newInt32(int32(index))...)

	putOffsetIndex(b, offsets, 2, uint32(len(b)))
	b = append(b, newLocalArray(params)...)

	putOffsetIndex(b, offsets, 3, uint32(len(b)))
	b = append(b, newLocal(ret)...)
	putOffsetIndex(b, offsets, 4, uint32(len(b)))
	b = append(b, newStringArray(path)...)

	putOffsetIndex(b, offsets, 5, uint32(len(b)))
	b = append(b, blocks...)

	putUint32(b, lengthOffset, uint32(len(b))) // Update the length

	return b
}

func (f function) IsBuiltin() bool {
	return getUint32(f, 4) == typeBuiltin
}

func (f function) Name() string {
	offset := getOffsetIndex(f, 8, 0)
	return getString(f, offset)
}

func (f function) Index() int {
	offset := getOffsetIndex(f, 8, 1)
	return int(getInt32(f, offset))
}

func (f function) Params() []Local {
	offset := getOffsetIndex(f, 8, 2)
	return getLocalArray(f, offset)
}

func (f function) Return() Local {
	offset := getOffsetIndex(f, 8, 3)
	return getLocal(f, offset)
}

func (f function) Path() []string {
	offset := getOffsetIndex(f, 8, 4)
	return getStringArray(f, offset)
}

func (f function) Blocks() blocks {
	offset := getOffsetIndex(f, 8, 5)
	return blocks(f[offset:])
}

func (builtin) Write(name string, relation bool) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeBuiltin),
		newBool(relation),
		newString(name),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (b builtin) Name() string {
	return getString(b, 9)
}

func (b builtin) Relation() bool {
	return getBool(b, 8)
}

func (plans) Write(plans [][]byte) []byte {
	n := len(plans)

	offsets := uint32(4)
	b := concat(
		newUint32(uint32(n)),
		newOffsetIndex(n),
	)

	for i, plan := range plans {
		putOffsetIndex(b, offsets, i, uint32(len(b)))
		b = append(b, plan...)
	}

	return b
}

func (p plans) Len() int {
	return int(getUint32(p, 0))
}

func (p plans) Plan(i int) plan {
	offset := getOffsetIndex(p, 4, i)
	l := getUint32(p, offset)

	return plan(p[offset : offset+l])
}

func (plan) Write(name string, blocks []byte) []byte {
	offset := uint32(4)
	b := concat(
		newUint32(0), // Length placeholder.
		newOffsetIndex(2),
	)

	putOffsetIndex(b, offset, 0, uint32(len(b)))
	b = append(b, newString(name)...)

	putOffsetIndex(b, offset, 1, uint32(len(b)))
	b = append(b, blocks...)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (p plan) Name() string {
	offset := getOffsetIndex(p, 4, 0)
	return getString(p, offset)
}

func (p plan) Blocks() blocks {
	offset := getOffsetIndex(p, 4, 1)
	return blocks(p[offset:])
}

func (blocks) Write(blocks [][]byte) []byte {
	lengthOffset := uint32(0)
	offsets := uint32(8)

	data := concat(
		newUint32(0),                   // 0: Length placeholder.
		newUint32(uint32(len(blocks))), // 4: # of blocks
		newOffsetIndex(len(blocks)))    // 8: offsets

	for i := range blocks {
		putOffsetIndex(data, offsets, i, uint32(len(data)))
		data = append(data, blocks[i]...)
	}

	putUint32(data, lengthOffset, uint32(len(data))) // Update the length
	return data
}

func (b blocks) Len() int {
	return int(getUint32(b, 4))
}

func (b blocks) Block(i int) block {
	offset := getOffsetIndex(b, 8, i)
	l := getUint32(b, offset)
	return block(b[offset : offset+l])
}

func (block) Write(stmts [][]byte) []byte {
	lengthOffset := uint32(0)
	offsets := uint32(8)

	b := concat(
		newUint32(0),                  // Length placeholder.
		newUint32(uint32(len(stmts))), // # of statements
		newOffsetIndex(len(stmts)),
	)

	for i, data := range stmts {
		putOffsetIndex(b, offsets, i, uint32(len(b)))
		b = append(b, data...)
	}

	putUint32(b, lengthOffset, uint32(len(b))) // Update the length
	return b
}

func (b block) Statements() statements {
	return statements(b)
}

func (s statements) Len() int {
	return int(getUint32(s, 4))
}

func (s statements) Statement(i int) statement {
	offset := getOffsetIndex(s, 8, i)
	l := getUint32(s, offset)
	return statement(s[offset : offset+l])
}

func (s statement) Type() uint32 {
	return getUint32(s, 4)
}

// Statements

func (assignInt) Write(value int64, target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementAssignInt),
		newInt64(value),
		newLocal(target),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (a assignInt) Len() uint32 {
	return getLen(a)
}

func (a assignInt) Type() uint32 {
	return getType(a)
}

func (a assignInt) Value() int64 {
	return getInt64(a, 8)
}

func (a assignInt) Target() Local {
	return getLocal(a, 16)
}

func (assignVar) Write(source LocalOrConst, target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementAssignVar),
		newLocal(target),
		newLocalOrConst(source),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (a assignVar) Len() uint32 {
	return getLen(a)
}

func (a assignVar) Type() uint32 {
	return getType(a)
}

func (a assignVar) Source() LocalOrConst {
	return getLocalOrConst(a, 12)
}

func (a assignVar) Target() Local {
	return getLocal(a, 8)
}

func (assignVarOnce) Write(source LocalOrConst, target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementAssignVarOnce),
		newLocal(target),
		newLocalOrConst(source),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (a assignVarOnce) Len() uint32 {
	return getLen(a)
}

func (a assignVarOnce) Type() uint32 {
	return getType(a)
}

func (a assignVarOnce) Source() LocalOrConst {
	return getLocalOrConst(a, 12)
}

func (a assignVarOnce) Target() Local {
	return getLocal(a, 8)
}

func (blockStmt) Write(blocks []byte) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementBlockStmt),
		blocks,
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (b blockStmt) Len() uint32 {
	return getLen(b)
}

func (b blockStmt) Type() uint32 {
	return getType(b)
}

func (b blockStmt) Blocks() blocks {
	return blocks(b[8:])
}

func (breakStmt) Write(index uint32) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementBreakStmt),
		newUint32(index),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (b breakStmt) Len() uint32 {
	return getLen(b)
}

func (b breakStmt) Type() uint32 {
	return getType(b)
}

func (b breakStmt) Index() uint32 {
	return getUint32(b, 8)
}

func (c callDynamic) Write(args []Local, result Local, path []LocalOrConst) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementCallDynamic),
		newLocal(result),
		newLocalArray(args),
		newLocalOrConstArray(path),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (c callDynamic) Len() uint32 {
	return getLen(c)
}

func (c callDynamic) Type() uint32 {
	return getType(c)
}

func (c callDynamic) Args() []Local {
	return getLocalArray(c, 12)
}

func (c callDynamic) Result() Local {
	return getLocal(c, 8)
}

func (c callDynamic) Path() []LocalOrConst {
	n := getLocalArraySize(c, 12)
	return getLocalOrConstArray(c, 12+uint32(n))
}

func (c call) Write(index int, args []LocalOrConst, result Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementCall),
		newLocal(result),
		newLocalOrConstArray(args),
		newUint32(uint32(index)),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (c call) Len() uint32 {
	return getLen(c)
}

func (c call) Type() uint32 {
	return getType(c)
}

func (c call) Func() int {
	n := getLocalOrConstArraySize(c, 12)
	return int(getUint32(c, 12+uint32(n)))
}

func (c call) Args() []LocalOrConst {
	return getLocalOrConstArray(c, 12)
}

func (c call) Result() Local {
	return getLocal(c, 8)
}

func (d dot) Write(source LocalOrConst, key LocalOrConst, target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementDot),
		newLocal(target),
		newLocalOrConst(source),
		newLocalOrConst(key),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (d dot) Len() uint32 {
	return getLen(d)
}

func (d dot) Type() uint32 {
	return getType(d)
}

func (d dot) Source() LocalOrConst {
	return getLocalOrConst(d, 12)
}

func (d dot) Key() LocalOrConst {
	return getLocalOrConst(d, 12+5)
}

func (d dot) Target() Local {
	return getLocal(d, 8)
}

func (e equal) Write(aa LocalOrConst, bb LocalOrConst) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementEqual),
		newLocalOrConst(aa),
		newLocalOrConst(bb),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (e equal) Len() uint32 {
	return getLen(e)
}

func (e equal) Type() uint32 {
	return getType(e)
}

func (e equal) A() LocalOrConst {
	return getLocalOrConst(e, 8)
}

func (e equal) B() LocalOrConst {
	return getLocalOrConst(e, 8+5)
}

func (i isArray) Write(source LocalOrConst) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementIsArray),
		newLocalOrConst(source),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (i isArray) Len() uint32 {
	return getLen(i)
}

func (i isArray) Type() uint32 {
	return getType(i)
}

func (i isArray) Source() LocalOrConst {
	return getLocalOrConst(i, 8)
}

func (i isObject) Write(source LocalOrConst) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementIsObject),
		newLocalOrConst(source),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (i isObject) Len() uint32 {
	return getLen(i)
}

func (i isObject) Type() uint32 {
	return getType(i)
}

func (i isObject) Source() LocalOrConst {
	return getLocalOrConst(i, 8)
}

func (i isDefined) Write(source Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementIsDefined),
		newLocal(source),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (i isDefined) Len() uint32 {
	return getLen(i)
}

func (i isDefined) Type() uint32 {
	return getType(i)
}

func (i isDefined) Source() Local {
	return getLocal(i, 8)
}

func (i isUndefined) Write(source Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementIsUndefined),
		newLocal(source),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (i isUndefined) Len() uint32 {
	return getLen(i)
}

func (i isUndefined) Type() uint32 {
	return getType(i)
}

func (i isUndefined) Source() Local {
	return getLocal(i, 8)
}

func (m makeNull) Write(target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementMakeNull),
		newLocal(target),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (m makeNull) Len() uint32 {
	return getLen(m)
}

func (m makeNull) Type() uint32 {
	return getType(m)
}

func (m makeNull) Target() Local {
	return getLocal(m, 8)
}

func (m makeNumberInt) Write(value int64, target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementMakeNumberInt),
		newInt64(value),
		newLocal(target),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (m makeNumberInt) Len() uint32 {
	return getLen(m)
}

func (m makeNumberInt) Type() uint32 {
	return getType(m)
}

func (m makeNumberInt) Value() int64 {
	return getInt64(m, 8)
}

func (m makeNumberInt) Target() Local {
	return getLocal(m, 16)
}

func (m makeNumberRef) Write(index int, target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementMakeNumberRef),
		newUint32(uint32(index)),
		newLocal(target),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (m makeNumberRef) Len() uint32 {
	return getLen(m)
}

func (m makeNumberRef) Type() uint32 {
	return getType(m)
}

func (m makeNumberRef) Index() int {
	return int(getUint32(m, 8))
}

func (m makeNumberRef) Target() Local {
	return getLocal(m, 12)
}

func (m makeArray) Write(capacity int32, target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementMakeArray),
		newInt32(capacity),
		newLocal(target),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (m makeArray) Len() uint32 {
	return getLen(m)
}

func (m makeArray) Type() uint32 {
	return getType(m)
}

func (m makeArray) Capacity() int32 {
	return getInt32(m, 8)
}

func (m makeArray) Target() Local {
	return getLocal(m, 12)
}

func (m makeObject) Write(target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementMakeObject),
		newLocal(target),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (m makeObject) Len() uint32 {
	return getLen(m)
}

func (m makeObject) Type() uint32 {
	return getType(m)
}

func (m makeObject) Target() Local {
	return getLocal(m, 8)
}

func (m makeSet) Write(target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementMakeSet),
		newLocal(target),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (m makeSet) Len() uint32 {
	return getLen(m)
}

func (m makeSet) Type() uint32 {
	return getType(m)
}

func (m makeSet) Target() Local {
	return getLocal(m, 8)
}

func (notEqual) Write(aa LocalOrConst, bb LocalOrConst) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementNotEqual),
		newLocalOrConst(aa),
		newLocalOrConst(bb),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (n notEqual) Len() uint32 {
	return getLen(n)
}

func (n notEqual) Type() uint32 {
	return getType(n)
}

func (n notEqual) A() LocalOrConst {
	return getLocalOrConst(n, 8)
}

func (n notEqual) B() LocalOrConst {
	return getLocalOrConst(n, 8+5)
}

func (l lenStmt) Write(source LocalOrConst, target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementLen),
		newLocal(target),
		newLocalOrConst(source),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (l lenStmt) Len() uint32 {
	return getLen(l)
}

func (l lenStmt) Type() uint32 {
	return getType(l)
}

func (l lenStmt) Source() LocalOrConst {
	return getLocalOrConst(l, 12)
}

func (l lenStmt) Target() Local {
	return getLocal(l, 8)
}

func (a arrayAppend) Write(value LocalOrConst, array Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementArrayAppend),
		newLocal(array),
		newLocalOrConst(value),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (a arrayAppend) Len() uint32 {
	return getLen(a)
}

func (a arrayAppend) Type() uint32 {
	return getType(a)
}

func (a arrayAppend) Value() LocalOrConst {
	return getLocalOrConst(a, 12)
}

func (a arrayAppend) Array() Local {
	return getLocal(a, 8)
}

func (s setAdd) Write(value LocalOrConst, set Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementSetAdd),
		newLocal(set),
		newLocalOrConst(value),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (s setAdd) Len() uint32 {
	return getLen(s)
}

func (s setAdd) Type() uint32 {
	return getType(s)
}

func (s setAdd) Value() LocalOrConst {
	return getLocalOrConst(s, 12)
}

func (s setAdd) Set() Local {
	return getLocal(s, 8)
}

func (o objectInsertOnce) Write(key, value LocalOrConst, obj Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementObjectInsertOnce),
		newLocal(obj),
		newLocalOrConst(key),
		newLocalOrConst(value),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (o objectInsertOnce) Len() uint32 {
	return getLen(o)
}

func (o objectInsertOnce) Type() uint32 {
	return getType(o)
}

func (o objectInsertOnce) Key() LocalOrConst {
	return getLocalOrConst(o, 12)
}

func (o objectInsertOnce) Value() LocalOrConst {
	return getLocalOrConst(o, 17)
}

func (o objectInsertOnce) Object() Local {
	return getLocal(o, 8)
}

func (o objectInsert) Write(key, value LocalOrConst, obj Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementObjectInsert),
		newLocal(obj),
		newLocalOrConst(key),
		newLocalOrConst(value),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (o objectInsert) Len() uint32 {
	return getLen(o)
}

func (o objectInsert) Type() uint32 {
	return getType(o)
}

func (o objectInsert) Key() LocalOrConst {
	return getLocalOrConst(o, 12)
}

func (o objectInsert) Value() LocalOrConst {
	return getLocalOrConst(o, 17)
}

func (o objectInsert) Object() Local {
	return getLocal(o, 8)
}

func (o objectMerge) Write(a, b, target Local) []byte {
	r := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementObjectMerge),
		newLocal(a),
		newLocal(b),
		newLocal(target),
	)

	putUint32(r, 0, uint32(len(r))) // Update the length
	return r
}

func (o objectMerge) Len() uint32 {
	return getLen(o)
}

func (o objectMerge) Type() uint32 {
	return getType(o)
}

func (o objectMerge) A() Local {
	return getLocal(o, 8)
}

func (o objectMerge) B() Local {
	return getLocal(o, 12)
}

func (o objectMerge) Target() Local {
	return getLocal(o, 16)
}

func (nop) Write() []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementNop),
	)
	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (n nop) Len() uint32 {
	return getLen(n)
}

func (n nop) Type() uint32 {
	return getType(n)
}

func (not) Write(block []byte) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementNot),
		block,
	)
	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (n not) Len() uint32 {
	return getLen(n)
}

func (n not) Type() uint32 {
	return getType(n)
}

func (n not) Block() block {
	return block(n[8:])
}

func (resetLocal) Write(target Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementResetLocal),
		newLocal(target),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (r resetLocal) Len() uint32 {
	return getLen(r)
}

func (r resetLocal) Type() uint32 {
	return getType(r)
}

func (r resetLocal) Target() Local {
	return getLocal(r, 8)
}

func (resultSetAdd) Write(value Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementResultSetAdd),
		newLocal(value),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (r resultSetAdd) Len() uint32 {
	return getLen(r)
}

func (r resultSetAdd) Type() uint32 {
	return getType(r)
}

func (r resultSetAdd) Value() Local {
	return getLocal(r, 8)
}

func (returnLocal) Write(source Local) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementReturnLocal),
		newLocal(source),
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (r returnLocal) Len() uint32 {
	return getLen(r)
}

func (r returnLocal) Type() uint32 {
	return getType(r)
}

func (r returnLocal) Source() Local {
	return getLocal(r, 8)
}

func (scan) Write(source, key, value Local, block []byte) []byte {
	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementScan),
		newLocal(source),
		newLocal(key),
		newLocal(value),
		block,
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (s scan) Len() uint32 {
	return getLen(s)
}

func (s scan) Type() uint32 {
	return getType(s)
}

func (s scan) Source() Local {
	return getLocal(s, 8)
}

func (s scan) Key() Local {
	return getLocal(s, 12)
}

func (s scan) Value() Local {
	return getLocal(s, 16)
}

func (s scan) Block() block {
	return block(s[20:])
}

func (with) Write(local Local, path []int, value LocalOrConst, block []byte) []byte {
	a := make([]int32, len(path))
	for i, v := range path {
		a[i] = int32(v)
	}

	b := concat(
		newUint32(0), // Length placeholder.
		newUint32(typeStatementWith),
		newLocal(local),
		newInt32Array(a),
		newLocalOrConst(value),
		block,
	)

	putUint32(b, 0, uint32(len(b))) // Update the length
	return b
}

func (w with) Len() uint32 {
	return getLen(w)
}

func (w with) Type() uint32 {
	return getType(w)
}

func (w with) Local() Local {
	return getLocal(w, 8)
}

func (w with) Path() []int {
	a := getInt32Array(w, 12)

	paths := make([]int, len(a))
	for i, p := range a {
		paths[i] = int(p)
	}

	return paths
}

func (w with) Value() LocalOrConst {
	n := getInt32ArraySize(w, 12)
	return getLocalOrConst(w, 12+uint32(n))
}

func (w with) Block() block {
	n := getInt32ArraySize(w, 12)
	return block(w[12+int(n)+5:])
}

// Primitive types

func newBool(value bool) []byte {
	if value {
		return []byte{1}
	}

	return []byte{0}
}

func getBool(data []byte, offset uint32) bool {
	return data[offset] != 0
}

func getOffsetIndex(data []byte, offset uint32, i int) uint32 {
	return getUint32(data, offset+uint32(i)*4)
}

func newOffsetIndex(n int) []byte {
	return make([]byte, n*4)
}

func putOffsetIndex(data []byte, offset uint32, i int, value uint32) {
	binary.BigEndian.PutUint32(data[offset+uint32(i*4):], value)
}

func getUint32(data []byte, offset uint32) uint32 {
	return binary.BigEndian.Uint32(data[offset:])
}

func newUint32(value uint32) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, value)
	return data
}

func putUint32(data []byte, offset uint32, value uint32) uint32 {
	binary.BigEndian.PutUint32(data[offset:offset+4], value)
	return 4
}

func getInt32(data []byte, offset uint32) int32 {
	return int32(binary.BigEndian.Uint32(data[offset:]))
}

func newInt32(value int32) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(value))
	return data
}

func getInt64(data []byte, offset uint32) int64 {
	return int64(binary.BigEndian.Uint64(data[offset:]))
}

func newInt64(value int64) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, uint64(value))
	return data
}

func getInt32Array(data []byte, offset uint32) []int32 {
	n := getUint32(data, offset)

	a := make([]int32, 0, n)
	for i := uint32(0); i < n; i++ {
		a = append(a, getInt32(data, offset+4+i*4))
	}

	return a
}

func newInt32Array(value []int32) []byte {
	data := newUint32(uint32(len(value)))
	for _, v := range value {
		data = append(data, newInt32(v)...)
	}

	return data
}

func getInt32ArrayLen(data []byte, offset uint32) int {
	n := getUint32(data, offset)
	return int(n)
}

func getInt32ArraySize(data []byte, offset uint32) int {
	return 4 + 4*getInt32ArrayLen(data, offset)
}

func getString(data []byte, offset uint32) string {
	l := getUint32(data, offset)
	offset += 4
	return string(data[offset : offset+l])
}

func newString(value string) []byte {
	data := []byte(value)
	return append(newUint32(uint32(len(data))), data...)
}

func newStringArray(value []string) []byte {
	data := newUint32(uint32(len(value)))

	offsets := uint32(len(data))
	data = append(data, newOffsetIndex(len(value))...)

	for i, s := range value {
		putOffsetIndex(data, offsets, i, uint32(len(data)))
		data = append(data, newString(s)...)
	}

	return data
}

func getStringArray(data []byte, offset uint32) []string {
	data = data[offset:]

	n := getUint32(data, 0)

	a := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		stringOffset := getOffsetIndex(data, 4, int(i))
		a = append(a, getString(data, stringOffset))
	}

	return a
}

func getLen(data []byte) uint32 {
	return getUint32(data, 0)
}

func getType(data []byte) uint32 {
	return getUint32(data, 4)
}

func newLocal(local Local) []byte {
	return newUint32(uint32(local))
}

func getLocal(data []byte, offset uint32) Local {
	return Local(getUint32(data, offset))
}

func getLocalArray(data []byte, offset uint32) []Local {
	a := getInt32Array(data, offset)
	r := make([]Local, len(a))

	for i := range a {
		r[i] = Local(a[i])
	}

	return r
}

func getLocalArraySize(data []byte, offset uint32) int {
	return getInt32ArraySize(data, offset)
}

func newLocalArray(local []Local) []byte {
	a := make([]int32, len(local))
	for i := range local {
		a[i] = int32(local[i])
	}

	return newInt32Array(a)
}

const (
	localType            = 0
	boolConstType        = 1
	stringIndexConstType = 2
)

func newLocalOrConst(lc LocalOrConst) []byte {
	switch v := lc.(type) {
	case Local:
		return concat(
			[]byte{localType},
			newUint32(uint32(v)),
		)

	case BoolConst:
		var t uint32
		if v {
			t = 1
		}
		return concat(
			[]byte{boolConstType},
			newUint32(t),
		)

	case StringIndexConst:
		return concat(
			[]byte{stringIndexConstType},
			newUint32(uint32(v)),
		)

	default:
		panic("unsupported local or const")
	}
}

func getLocalOrConst(data []byte, offset uint32) LocalOrConst {
	switch data[offset] {
	case localType:
		return Local(getUint32(data[offset:], 1))

	case boolConstType:
		if v := getUint32(data[offset:], 1); v == 0 {
			return BoolConst(false)
		} else {
			return BoolConst(true)
		}

	case stringIndexConstType:
		return StringIndexConst(getUint32(data[offset:], 1))

	default:
		panic("unsupported local or const")
	}
}

func getLocalOrConstArray(data []byte, offset uint32) []LocalOrConst {
	n := getUint32(data, offset)

	l := make([]LocalOrConst, 0, n)
	for i := uint32(0); i < n; i++ {
		l = append(l, getLocalOrConst(data, offset+4+i*5))
	}

	return l
}

func getLocalOrConstArrayLen(data []byte, offset uint32) int {
	n := getUint32(data, offset)
	return int(n)
}

func getLocalOrConstArraySize(data []byte, offset uint32) int {
	return 4 + 5*getLocalOrConstArrayLen(data, offset)
}

func newLocalOrConstArray(l []LocalOrConst) []byte {
	data := newUint32(uint32(len(l)))
	for _, l := range l {
		data = append(data, newLocalOrConst(l)...)
	}

	return data
}

func concat(fields ...[]byte) []byte {
	l := 0
	for _, field := range fields {
		l += len(field)
	}
	result := make([]byte, 0, l)

	for _, field := range fields {
		result = append(result, field...)
	}

	return result
}
