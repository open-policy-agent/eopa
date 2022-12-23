package vm

import (
	"bytes"
	"encoding/binary"
	"fmt"
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

func (header) Write(version uint32, totalLength uint32, stringsOffset uint32, functionsOffset uint32, plansOffset uint32) []byte {

	l := 4 + 4 + 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = append(d, []byte(magic)...)
	d = appendUint32(d, version)
	d = appendUint32(d, totalLength)
	d = appendUint32(d, stringsOffset)
	d = appendUint32(d, functionsOffset)
	d = appendUint32(d, plansOffset)

	if len(d) != l {
		panic(fmt.Sprint("header", l, len(d)))
	}
	return d
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
	l := len(strings) + len(functions) + len(plans)
	d := make([]byte, 0, l)

	stringsOffset := uint32(len(d))
	d = append(d, strings...)

	functionsOffset := uint32(len(d))
	d = append(d, functions...)

	plansOffset := uint32(len(d))
	d = append(d, plans...)

	if len(d) != l {
		panic(fmt.Sprint("executable", l, len(d)))
	}

	return append(header{}.Write(version, headerLength+uint32(len(d)), stringsOffset, functionsOffset, plansOffset), d...)
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

func (strings) Write(strings []string) []byte {
	n := len(strings)
	l := 4 + appendOffsetSize(n)
	for _, s := range strings {
		l += appendStringSize(s)
	}
	d := make([]byte, 0, l)

	offset := uint32(4)
	d = appendUint32(d, uint32(n))
	d = appendOffsetIndex(d, n)

	for i, s := range strings {
		putOffsetIndex(d, offset, i, uint32(len(d)))
		d = appendString(d, s)
	}
	if l != len(d) {
		panic(fmt.Sprint("strings", l, len(d)))
	}
	return d
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
	l := 4 + 4 + appendOffsetSize(6) + appendStringSize(name) + 4 + appendLocalArraySize(params) + 4 + appendStringArraySize(path) + len(blocks)
	d := make([]byte, 0, l)

	offset := uint32(8)
	lengthOffset := uint32(0)
	d = appendUint32(d, 0) // Length
	d = appendUint32(d, typeFunction)
	d = appendOffsetIndex(d, 6)

	putOffsetIndex(d, offset, 0, uint32(len(d)))
	d = appendString(d, name)

	putOffsetIndex(d, offset, 1, uint32(len(d)))
	d = appendInt32(d, int32(index))

	putOffsetIndex(d, offset, 2, uint32(len(d)))
	d = appendLocalArray(d, params)

	putOffsetIndex(d, offset, 3, uint32(len(d)))
	d = appendLocal(d, ret)

	putOffsetIndex(d, offset, 4, uint32(len(d)))
	d = appendStringArray(d, path)

	putOffsetIndex(d, offset, 5, uint32(len(d)))
	d = append(d, blocks...)

	if l != len(d) {
		panic(fmt.Sprint("function", l, len(d)))
	}

	putUint32(d, lengthOffset, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 1 + appendStringSize(name)
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // length
	d = appendUint32(d, typeBuiltin)
	d = appendBool(d, relation)
	d = appendString(d, name)

	if l != len(d) {
		panic(fmt.Sprint("builtin", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
}

func (b builtin) Name() string {
	return getString(b, 9)
}

func (b builtin) Relation() bool {
	return getBool(b, 8)
}

func (plans) Write(plans [][]byte) []byte {
	n := len(plans)

	l := 4 + appendOffsetSize(n)
	for _, plan := range plans {
		l += len(plan)
	}
	d := make([]byte, 0, l)

	offset := uint32(4)
	d = appendUint32(d, uint32(n))
	d = appendOffsetIndex(d, n)

	for i, plan := range plans {
		putOffsetIndex(d, offset, i, uint32(len(d)))
		d = append(d, plan...)
	}
	if l != len(d) {
		panic(fmt.Sprint("plans", l, len(d)))
	}

	return d
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
	l := 4 + appendOffsetSize(2) + appendStringSize(name) + len(blocks)
	d := make([]byte, 0, l)

	offset := uint32(4)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendOffsetIndex(d, 2)

	putOffsetIndex(d, offset, 0, uint32(len(d)))
	d = appendString(d, name)

	putOffsetIndex(d, offset, 1, uint32(len(d)))
	d = append(d, blocks...)

	if l != len(d) {
		panic(fmt.Sprint("plan", l, len(d)))
	}
	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	n := len(blocks)

	lengthOffset := uint32(0)

	l := 4 + 4 + appendOffsetSize(n)
	for _, v := range blocks {
		l += len(v)
	}

	d := make([]byte, 0, l)

	offset := uint32(8)
	d = appendUint32(d, 0)         // 0: Length placeholder.
	d = appendUint32(d, uint32(n)) // 4: # of blocks
	d = appendOffsetIndex(d, n)    // 8: offsets

	for i := range blocks {
		putOffsetIndex(d, offset, i, uint32(len(d)))
		d = append(d, blocks[i]...)
	}
	if l != len(d) {
		panic(fmt.Sprint("blocks", l, len(d)))
	}

	putUint32(d, lengthOffset, uint32(len(d))) // Update the length
	return d
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

	n := len(stmts)

	l := 4 + 4 + appendOffsetSize(n)
	for _, data := range stmts {
		l += len(data)
	}

	d := make([]byte, 0, l)

	offset := uint32(8)
	d = appendUint32(d, 0)         // Length placeholder.
	d = appendUint32(d, uint32(n)) // # of statements
	d = appendOffsetIndex(d, n)

	for i, data := range stmts {
		putOffsetIndex(d, offset, i, uint32(len(d)))
		d = append(d, data...)
	}
	if l != len(d) {
		panic(fmt.Sprint("block", l, len(d)))
	}

	putUint32(d, lengthOffset, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 8 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementAssignInt)
	d = appendInt64(d, value)
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("asignInt", l, len(d)))
	}
	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 4 + 5
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementAssignVar)
	d = appendLocal(d, target)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprint("assignVar", l, len(d)))
	}
	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 4 + 5
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementAssignVarOnce)
	d = appendLocal(d, target)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprint("assignVarOnce", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + len(blocks)
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementBlockStmt)
	d = append(d, blocks...)

	if l != len(d) {
		panic(fmt.Sprint("blockStmt", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementBreakStmt)
	d = appendUint32(d, index)

	if l != len(d) {
		panic(fmt.Sprint("block", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (callDynamic) Write(args []Local, result Local, path []LocalOrConst) []byte {
	l := 4 + 4 + 4 + appendLocalArraySize(args) + appendLocalOrConstArraySize(path)

	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementCallDynamic)
	d = appendLocal(d, result)
	d = appendLocalArray(d, args)
	d = appendLocalOrConstArray(d, path)

	if l != len(d) {
		panic(fmt.Sprint("callDynamic", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (call) Write(index int, args []LocalOrConst, result Local) []byte {
	l := 4 + 4 + 4 + appendLocalOrConstArraySize(args) + 4
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementCall)
	d = appendLocal(d, result)
	d = appendLocalOrConstArray(d, args)
	d = appendUint32(d, uint32(index))

	if l != len(d) {
		panic(fmt.Sprint("call", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (dot) Write(source LocalOrConst, key LocalOrConst, target Local) []byte {
	l := 4 + 4 + 4 + 5 + 5
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementDot)
	d = appendLocal(d, target)
	d = appendLocalOrConst(d, source)
	d = appendLocalOrConst(d, key)

	if l != len(d) {
		panic(fmt.Sprint("dot", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (equal) Write(aa LocalOrConst, bb LocalOrConst) []byte {
	l := 4 + 4 + 5 + 5
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementEqual)
	d = appendLocalOrConst(d, aa)
	d = appendLocalOrConst(d, bb)

	if l != len(d) {
		panic(fmt.Sprint("equal", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (isArray) Write(source LocalOrConst) []byte {
	l := 4 + 4 + 5
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementIsArray)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprint("isArray", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (isObject) Write(source LocalOrConst) []byte {
	l := 4 + 4 + 5
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementIsObject)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprint("isObject", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (isDefined) Write(source Local) []byte {
	l := 4 + 4 + 4
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementIsDefined)
	d = appendLocal(d, source)

	if l != len(d) {
		panic(fmt.Sprint("isDefined", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (isUndefined) Write(source Local) []byte {
	l := 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementIsUndefined)
	d = appendLocal(d, source)

	if l != len(d) {
		panic(fmt.Sprint("isUndefined", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (makeNull) Write(target Local) []byte {
	l := 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementMakeNull)
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("makeNull", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (makeNumberInt) Write(value int64, target Local) []byte {
	l := 4 + 4 + 8 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementMakeNumberInt)
	d = appendInt64(d, value)
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("makeNumberInt", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (makeNumberRef) Write(index int, target Local) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementMakeNumberRef)
	d = appendUint32(d, uint32(index))
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("makeNumberRef", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (makeArray) Write(capacity int32, target Local) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementMakeArray)
	d = appendInt32(d, capacity)
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("makeArray", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (makeObject) Write(target Local) []byte {
	l := 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementMakeObject)
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("makeObject", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (makeSet) Write(target Local) []byte {
	l := 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementMakeSet)
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("makeSet", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 5 + 5
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementNotEqual)
	d = appendLocalOrConst(d, aa)
	d = appendLocalOrConst(d, bb)

	if l != len(d) {
		panic(fmt.Sprint("notEqual", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (lenStmt) Write(source LocalOrConst, target Local) []byte {
	l := 4 + 4 + 4 + 5
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementLen)
	d = appendLocal(d, target)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprint("lenStmt", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (arrayAppend) Write(value LocalOrConst, array Local) []byte {
	l := 4 + 4 + 4 + 5
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementArrayAppend)
	d = appendLocal(d, array)
	d = appendLocalOrConst(d, value)

	if l != len(d) {
		panic(fmt.Sprint("arrayAppend", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (setAdd) Write(value LocalOrConst, set Local) []byte {
	l := 4 + 4 + 4 + 5
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementSetAdd)
	d = appendLocal(d, set)
	d = appendLocalOrConst(d, value)

	if l != len(d) {
		panic(fmt.Sprint("setAdd", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (objectInsertOnce) Write(key, value LocalOrConst, obj Local) []byte {
	l := 4 + 4 + 4 + 5 + 5
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementObjectInsertOnce)
	d = appendLocal(d, obj)
	d = appendLocalOrConst(d, key)
	d = appendLocalOrConst(d, value)

	if l != len(d) {
		panic(fmt.Sprint("objectInsertOnce", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (objectInsert) Write(key, value LocalOrConst, obj Local) []byte {
	l := 4 + 4 + 4 + 5 + 5
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementObjectInsert)
	d = appendLocal(d, obj)
	d = appendLocalOrConst(d, key)
	d = appendLocalOrConst(d, value)

	if l != len(d) {
		panic(fmt.Sprint("objectInsert", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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

func (objectMerge) Write(a, b, target Local) []byte {
	l := 4 + 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementObjectMerge)
	d = appendLocal(d, a)
	d = appendLocal(d, b)
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("objectMerge", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementNop)

	if l != len(d) {
		panic(fmt.Sprint("nop", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
}

func (n nop) Len() uint32 {
	return getLen(n)
}

func (n nop) Type() uint32 {
	return getType(n)
}

func (not) Write(block []byte) []byte {
	l := 4 + 4 + len(block)
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementNot)
	d = append(d, block...)

	if l != len(d) {
		panic(fmt.Sprint("not", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementResetLocal)
	d = appendLocal(d, target)

	if l != len(d) {
		panic(fmt.Sprint("resetLocal", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementResultSetAdd)
	d = appendLocal(d, value)

	if l != len(d) {
		panic(fmt.Sprint("resultsSetAdd", l, len(d)))
	}
	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementReturnLocal)
	d = appendLocal(d, source)

	if l != len(d) {
		panic(fmt.Sprint("returnLocal", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 4 + 4 + 4 + len(block)
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementScan)
	d = appendLocal(d, source)
	d = appendLocal(d, key)
	d = appendLocal(d, value)
	d = append(d, block...)

	if l != len(d) {
		panic(fmt.Sprint("scan", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	l := 4 + 4 + 4 + appendIntArraySize(path) + 5 + len(block)
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementWith)
	d = appendLocal(d, local)
	d = appendIntArray(d, path)
	d = appendLocalOrConst(d, value)
	d = append(d, block...)

	if l != len(d) {
		panic(fmt.Sprint("with", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
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
	offset := uint32(12)
	n := getUint32(w, offset)

	a := make([]int, 0, n)
	for i := uint32(0); i < n; i++ {
		v := getInt32(w, offset+4+i*4)
		a = append(a, int(v))
	}

	return a
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

func appendBool(d []byte, value bool) []byte {
	if value {
		return append(d, byte(1))
	}

	return append(d, byte(0))
}

func getBool(data []byte, offset uint32) bool {
	return data[offset] != 0
}

func getOffsetIndex(data []byte, offset uint32, i int) uint32 {
	return getUint32(data, offset+uint32(i)*4)
}

func appendOffsetSize(n int) int {
	return n * 4
}

func appendOffsetIndex(d []byte, n int) []byte {
	l := make([]byte, 4)
	for i := 0; i < n; i++ {
		d = append(d, l...)
	}
	return d
}

func putOffsetIndex(data []byte, offset uint32, i int, value uint32) {
	binary.BigEndian.PutUint32(data[offset+uint32(i*4):], value)
}

func getUint32(data []byte, offset uint32) uint32 {
	return binary.BigEndian.Uint32(data[offset:])
}

func appendUint32(d []byte, value uint32) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, value)
	return append(d, data...)
}

func putUint32(data []byte, offset uint32, value uint32) uint32 {
	binary.BigEndian.PutUint32(data[offset:offset+4], value)
	return 4
}

func getInt32(data []byte, offset uint32) int32 {
	return int32(binary.BigEndian.Uint32(data[offset:]))
}

func appendInt32(d []byte, value int32) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(value))
	return append(d, data...)
}

func getInt64(data []byte, offset uint32) int64 {
	return int64(binary.BigEndian.Uint64(data[offset:]))
}

func appendInt64(d []byte, value int64) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, uint64(value))
	return append(d, data...)
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

func appendStringSize(value string) int {
	pad := 4 + len(value)
	return pad
}

func appendString(d []byte, value string) []byte {
	data := []byte(value)
	d = appendUint32(d, uint32(len(data)))
	return append(d, data...)
}

func appendStringArraySize(value []string) int {
	n := len(value)
	pad := 4 + appendOffsetSize(n)
	for _, s := range value {
		pad += appendStringSize(s)
	}
	return pad
}

func appendStringArray(d []byte, value []string) []byte {
	base := len(d)
	n := len(value)
	offsets := uint32(base + 4)

	d = appendUint32(d, uint32(n))
	d = appendOffsetIndex(d, n)

	for i, s := range value {
		putOffsetIndex(d, offsets, i, uint32(len(d)-base))
		d = appendString(d, s)
	}
	return d
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

func appendLocal(d []byte, local Local) []byte {
	return appendUint32(d, uint32(local))
}

func getLocal(data []byte, offset uint32) Local {
	return Local(getUint32(data, offset))
}

func getLocalArray(data []byte, offset uint32) []Local {
	n := getUint32(data, offset)

	a := make([]Local, 0, n)
	for i := uint32(0); i < n; i++ {
		a = append(a, getLocal(data, offset+4+i*4))
	}

	return a
}

func getLocalArraySize(data []byte, offset uint32) int {
	return getInt32ArraySize(data, offset)
}

func appendIntArraySize(local []int) int {
	return 4 + len(local)*4
}

func appendIntArray(d []byte, local []int) []byte {
	d = appendUint32(d, uint32(len(local)))
	for _, v := range local {
		d = appendInt32(d, int32(v))
	}
	return d
}

func appendLocalArraySize(local []Local) int {
	return 4 + len(local)*4
}

func appendLocalArray(d []byte, local []Local) []byte {
	d = appendUint32(d, uint32(len(local)))
	for _, v := range local {
		d = appendInt32(d, int32(v))
	}
	return d
}

const (
	localType            = 0
	boolConstType        = 1
	stringIndexConstType = 2
)

func appendLocalOrConst(d []byte, lc LocalOrConst) []byte {
	switch v := lc.(type) {
	case Local:
		d = append(d, localType)
		return appendUint32(d, uint32(v))

	case BoolConst:
		var t uint32
		if v {
			t = 1
		}
		d = append(d, boolConstType)
		return appendUint32(d, uint32(t))

	case StringIndexConst:
		d = append(d, stringIndexConstType)
		return appendUint32(d, uint32(v))

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

func appendLocalOrConstArraySize(l []LocalOrConst) int {
	return 4 + 5*len(l)
}

func appendLocalOrConstArray(d []byte, l []LocalOrConst) []byte {
	d = appendUint32(d, uint32(len(l)))
	for _, l := range l {
		d = appendLocalOrConst(d, l)
	}

	return d
}
