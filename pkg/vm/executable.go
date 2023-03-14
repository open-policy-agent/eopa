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

	if totalLength < headerLength {
		panic(fmt.Sprintf("headerLength %d %d", totalLength, headerLength))
	}

	d := make([]byte, 0, totalLength)
	d = append(d, []byte(magic)...)
	d = appendUint32(d, version)
	d = appendUint32(d, totalLength)
	d = appendUint32(d, stringsOffset)
	d = appendUint32(d, functionsOffset)
	d = appendUint32(d, plansOffset)

	if len(d) != headerLength {
		panic(fmt.Sprintf("header %d %d", headerLength, len(d)))
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
	stringsLen := len(strings)
	functionsLen := len(functions)
	plansLen := len(plans)

	l := headerLength + stringsLen + functionsLen + plansLen

	d := header{}.Write(version, uint32(l), uint32(0), uint32(stringsLen), uint32(stringsLen+functionsLen))

	d = append(d, strings...)
	d = append(d, functions...)
	d = append(d, plans...)

	if len(d) != l {
		panic(fmt.Sprintf("executable %d %d", l, len(d)))
	}
	return d
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

func (s strings) Len() int {
	return int(getUint32(s, 0))
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
		panic(fmt.Sprintf("strings %d %d", l, len(d)))
	}
	return d
}

//go:inline
func (s strings) String(vm *VM, i StringIndexConst) Value {
	if s, ok := vm.getCachedString(i); ok {
		return s
	}

	stringOffset := getOffsetIndex(s, 4, int(i))
	v := vm.ops.MakeString(getString(s, stringOffset))
	vm.setCachedString(i, v)
	return v
}

//go:inline
func (f functions) Len() int {
	return int(getUint32(f, 0))
}

//go:inline
func (f functions) Function(i int) function {
	offset := getOffsetIndex(f, 4, i)
	return function(f[offset:])
}

func (function) Write(name string, index int, params []Local, ret Local, blocks []byte, path []string) []byte {
	l := 4 + 4 + appendOffsetSize(4) + appendStringSize(name) + 4 + appendLocalArraySize(params) + 4 + appendStringArraySize(path) + len(blocks)
	d := make([]byte, 0, l)

	offset := uint32(16)
	lengthOffset := uint32(0)
	d = appendUint32(d, 0) // Length
	d = appendUint32(d, typeFunction)
	d = appendInt32(d, int32(index))
	d = appendLocal(d, ret)
	d = appendOffsetIndex(d, 4)

	putOffsetIndex(d, offset, 0, uint32(len(d)))
	d = appendLocalArray(d, params)

	putOffsetIndex(d, offset, 1, uint32(len(d)))
	d = appendString(d, name)

	putOffsetIndex(d, offset, 2, uint32(len(d)))
	d = appendStringArray(d, path)

	putOffsetIndex(d, offset, 3, uint32(len(d)))
	d = append(d, blocks...)

	if l != len(d) {
		panic(fmt.Sprintf("function %d %d", l, len(d)))
	}

	putUint32(d, lengthOffset, uint32(len(d))) // Update the length
	return d
}

//go:inline
func (f function) IsBuiltin() bool {
	return getUint32(f, 4) == typeBuiltin
}

//go:inline
func (f function) Name() string {
	offset := getOffsetIndex(f, 16, 1)
	return getString(f, offset)
}

func (f function) Index() int {
	return int(getInt32(f, 8))
}

func (f function) Params() []Local {
	return getLocalArray(f, uint32(16+appendOffsetSize(4)))
}

//go:inline
func (f function) ParamsLen() uint32 {
	return getUint32(f, uint32(16+appendOffsetSize(4)))
}

func (f function) ParamsIter(fcn func(i uint32, arg Local) error) error {
	offset := uint32(16 + appendOffsetSize(4))
	n := getUint32(f, offset)

	for i := uint32(0); i < n; i++ {
		if err := fcn(i, getLocal(f, offset+4+i*4)); err != nil {
			return err
		}
	}
	return nil
}

//go:inline
func (f function) Return() Local {
	return getLocal(f, 12)
}

func (f function) Path() []string {
	offset := getOffsetIndex(f, 16, 2)
	return getStringArray(f, offset)
}

//go:inline
func (f function) PathLen() uint32 {
	offset := getOffsetIndex(f, 16, 2)
	return getUint32(f, offset)
}

func (f function) PathIter(fcn func(i uint32, arg string) error) error {
	offset := getOffsetIndex(f, 16, 2)
	data := f[offset:]

	n := getUint32(data, 0)
	for i := uint32(0); i < n; i++ {
		stringOffset := getOffsetIndex(data, 4, int(i))
		if err := fcn(i, getString(data, stringOffset)); err != nil {
			return err
		}
	}
	return nil
}

//go:inline
func (f function) Blocks() blocks {
	offset := getOffsetIndex(f, 16, 3)
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
		panic(fmt.Sprintf("builtin %d %d", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
}

//go:inline
func (b builtin) Name() string {
	return getString(b, 9)
}

//go:inline
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
		panic(fmt.Sprintf("plans %d %d", l, len(d)))
	}

	return d
}

func (p plans) Len() int {
	return int(getUint32(p, 0))
}

func (p plans) Plan(i int) plan {
	offset := getOffsetIndex(p, 4, i)
	return plan(p[offset:])
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
		panic(fmt.Sprintf("plan %d %d", l, len(d)))
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
		panic(fmt.Sprintf("blocks %d %d", l, len(d)))
	}

	putUint32(d, lengthOffset, uint32(len(d))) // Update the length
	return d
}

//go:inline
func (b blocks) Len() int {
	return int(getUint32(b, 4))
}

//go:inline
func (b blocks) Block(i int) block {
	offset := getOffsetIndex(b, 8, i)
	return block(b[offset:])
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
		panic(fmt.Sprintf("block %d %d", l, len(d)))
	}

	putUint32(d, lengthOffset, uint32(len(d))) // Update the length
	return d
}

//go:inline
func (b block) Statements() statements {
	return statements(b)
}

//go:inline
func (s statements) Len() int {
	return int(getUint32(s, 4))
}

//go:inline
func (s statements) Statement(i int) statement {
	offset := getOffsetIndex(s, 8, i)
	return statement(s[offset:])
}

//go:inline
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
		panic(fmt.Sprintf("asignInt %d %d", l, len(d)))
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

//go:inline
func (a assignInt) Value() int64 {
	return getInt64(a, 8)
}

//go:inline
func (a assignInt) Target() Local {
	return getLocal(a, 16)
}

func (assignVar) Write(source LocalOrConst, target Local) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementAssignVar)
	d = appendLocal(d, target)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprintf("assignVar %d %d", l, len(d)))
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

//go:inline
func (a assignVar) Source() LocalOrConst {
	return getLocalOrConst(a, 12)
}

//go:inline
func (a assignVar) Target() Local {
	return getLocal(a, 8)
}

func (assignVarOnce) Write(source LocalOrConst, target Local) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementAssignVarOnce)
	d = appendLocal(d, target)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprintf("assignVarOnce %d %d", l, len(d)))
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

//go:inline
func (a assignVarOnce) Source() LocalOrConst {
	return getLocalOrConst(a, 12)
}

//go:inline
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
		panic(fmt.Sprintf("blockStmt %d %d", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
}

//go:inline
func (b blockStmt) Len() uint32 {
	return getLen(b)
}

func (b blockStmt) Type() uint32 {
	return getType(b)
}

//go:inline
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
		panic(fmt.Sprintf("block %d %d", l, len(d)))
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

//go:inline
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
		panic(fmt.Sprintf("callDynamic %d %d", l, len(d)))
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

//go:inline
func (c callDynamic) ArgsLen() uint32 {
	return getUint32(c, 12)
}

func (c callDynamic) ArgsIter(fcn func(i uint32, arg Local) error) error {
	offset := uint32(12)
	n := getUint32(c, offset)

	for i := uint32(0); i < n; i++ {
		if err := fcn(i, getLocal(c, offset+4+i*5)); err != nil {
			return err
		}
	}
	return nil
}

//go:inline
func (c callDynamic) Result() Local {
	return getLocal(c, 8)
}

func (c callDynamic) Path() []LocalOrConst {
	offset := uint32(getLocalArraySize(c, 12))
	return getLocalOrConstArray(c, 12+offset)
}

//go:inline
func (c callDynamic) PathLen() uint32 {
	offset := uint32(getLocalArraySize(c, 12))
	return getUint32(c, 12+offset)
}

func (c callDynamic) PathIter(fcn func(i uint32, arg LocalOrConst) error) error {
	offset := uint32(getLocalArraySize(c, 12)) + 12
	n := getUint32(c, offset)

	for i := uint32(0); i < n; i++ {
		if err := fcn(i, getLocalOrConst(c, offset+4+i*4)); err != nil {
			return err
		}
	}
	return nil
}

func (call) Write(index int, args []LocalOrConst, result Local) []byte {
	l := 4 + 4 + 4 + appendLocalOrConstArraySize(args) + 4
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementCall)
	d = appendLocal(d, result)
	d = appendUint32(d, uint32(index))
	d = appendLocalOrConstArray(d, args)

	if l != len(d) {
		panic(fmt.Sprintf("call %d %d", l, len(d)))
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

//go:inline
func (c call) Func() int {
	return int(getUint32(c, 12))
}

func (c call) Args() []LocalOrConst {
	return getLocalOrConstArray(c, 16)
}

//go:inline
func (c call) ArgsLen() uint32 {
	return getUint32(c, 16)
}

func (c call) ArgsIter(fcn func(i uint32, arg LocalOrConst) error) error {
	offset := uint32(16)
	n := getUint32(c, offset)

	for i := uint32(0); i < n; i++ {
		if err := fcn(i, getLocalOrConst(c, offset+4+i*4)); err != nil {
			return err
		}
	}
	return nil
}

//go:inline
func (c call) Result() Local {
	return getLocal(c, 8)
}

func (dot) Write(source LocalOrConst, key LocalOrConst, target Local) []byte {
	l := 4 + 4 + 4 + 4 + 4
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementDot)
	d = appendLocal(d, target)
	d = appendLocalOrConst(d, source)
	d = appendLocalOrConst(d, key)

	if l != len(d) {
		panic(fmt.Sprintf("dot %d %d", l, len(d)))
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

//go:inline
func (d dot) Source() LocalOrConst {
	return getLocalOrConst(d, 12)
}

//go:inline
func (d dot) Key() LocalOrConst {
	return getLocalOrConst(d, 12+4)
}

//go:inline
func (d dot) Target() Local {
	return getLocal(d, 8)
}

func (equal) Write(aa LocalOrConst, bb LocalOrConst) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementEqual)
	d = appendLocalOrConst(d, aa)
	d = appendLocalOrConst(d, bb)

	if l != len(d) {
		panic(fmt.Sprintf("equal %d %d", l, len(d)))
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

//go:inline
func (e equal) A() LocalOrConst {
	return getLocalOrConst(e, 8)
}

//go:inline
func (e equal) B() LocalOrConst {
	return getLocalOrConst(e, 8+4)
}

func (isArray) Write(source LocalOrConst) []byte {
	l := 4 + 4 + 4
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementIsArray)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprintf("isArray %d %d", l, len(d)))
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

//go:inline
func (i isArray) Source() LocalOrConst {
	return getLocalOrConst(i, 8)
}

func (isObject) Write(source LocalOrConst) []byte {
	l := 4 + 4 + 4
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementIsObject)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprintf("isObject %d %d", l, len(d)))
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

//go:inline
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
		panic(fmt.Sprintf("isDefined %d %d", l, len(d)))
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

//go:inline
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
		panic(fmt.Sprintf("isUndefined %d %d", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
}

func (i isUndefined) Len() uint32 {
	return getLen(i)
}

//go:inline
func (i isUndefined) Type() uint32 {
	return getType(i)
}

//go:inline
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
		panic(fmt.Sprintf("makeNull %d %d", l, len(d)))
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

//go:inline
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
		panic(fmt.Sprintf("makeNumberInt %d %d", l, len(d)))
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

//go:inline
func (m makeNumberInt) Value() int64 {
	return getInt64(m, 8)
}

//go:inline
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
		panic(fmt.Sprintf("makeNumberRef %d %d", l, len(d)))
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

//go:inline
func (m makeNumberRef) Index() int {
	return int(getUint32(m, 8))
}

//go:inline
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
		panic(fmt.Sprintf("makeArray %d %d", l, len(d)))
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

//go:inline
func (m makeArray) Capacity() int32 {
	return getInt32(m, 8)
}

//go:inline
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
		panic(fmt.Sprintf("makeObject %d %d", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
}

func (m makeObject) Len() uint32 {
	return getLen(m)
}

//go:inline
func (m makeObject) Type() uint32 {
	return getType(m)
}

//go:inline
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
		panic(fmt.Sprintf("makeSet %d %d", l, len(d)))
	}

	putUint32(d, 0, uint32(len(d))) // Update the length
	return d
}

func (m makeSet) Len() uint32 {
	return getLen(m)
}

//go:inline
func (m makeSet) Type() uint32 {
	return getType(m)
}

//go:inline
func (m makeSet) Target() Local {
	return getLocal(m, 8)
}

func (notEqual) Write(aa LocalOrConst, bb LocalOrConst) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementNotEqual)
	d = appendLocalOrConst(d, aa)
	d = appendLocalOrConst(d, bb)

	if l != len(d) {
		panic(fmt.Sprintf("notEqual %d %d", l, len(d)))
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

//go:inline
func (n notEqual) A() LocalOrConst {
	return getLocalOrConst(n, 8)
}

//go:inline
func (n notEqual) B() LocalOrConst {
	return getLocalOrConst(n, 8+4)
}

func (lenStmt) Write(source LocalOrConst, target Local) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementLen)
	d = appendLocal(d, target)
	d = appendLocalOrConst(d, source)

	if l != len(d) {
		panic(fmt.Sprintf("lenStmt %d %d", l, len(d)))
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

//go:inline
func (l lenStmt) Source() LocalOrConst {
	return getLocalOrConst(l, 12)
}

//go:inline
func (l lenStmt) Target() Local {
	return getLocal(l, 8)
}

func (arrayAppend) Write(value LocalOrConst, array Local) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)

	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementArrayAppend)
	d = appendLocal(d, array)
	d = appendLocalOrConst(d, value)

	if l != len(d) {
		panic(fmt.Sprintf("arrayAppend %d %d", l, len(d)))
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

//go:inline
func (a arrayAppend) Value() LocalOrConst {
	return getLocalOrConst(a, 12)
}

//go:inline
func (a arrayAppend) Array() Local {
	return getLocal(a, 8)
}

func (setAdd) Write(value LocalOrConst, set Local) []byte {
	l := 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementSetAdd)
	d = appendLocal(d, set)
	d = appendLocalOrConst(d, value)

	if l != len(d) {
		panic(fmt.Sprintf("setAdd %d %d", l, len(d)))
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

//go:inline
func (s setAdd) Value() LocalOrConst {
	return getLocalOrConst(s, 12)
}

//go:inline
func (s setAdd) Set() Local {
	return getLocal(s, 8)
}

func (objectInsertOnce) Write(key, value LocalOrConst, obj Local) []byte {
	l := 4 + 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementObjectInsertOnce)
	d = appendLocal(d, obj)
	d = appendLocalOrConst(d, key)
	d = appendLocalOrConst(d, value)

	if l != len(d) {
		panic(fmt.Sprintf("objectInsertOnce %d %d", l, len(d)))
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

//go:inline
func (o objectInsertOnce) Key() LocalOrConst {
	return getLocalOrConst(o, 12)
}

//go:inline
func (o objectInsertOnce) Value() LocalOrConst {
	return getLocalOrConst(o, 16)
}

//go:inline
func (o objectInsertOnce) Object() Local {
	return getLocal(o, 8)
}

func (objectInsert) Write(key, value LocalOrConst, obj Local) []byte {
	l := 4 + 4 + 4 + 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementObjectInsert)
	d = appendLocal(d, obj)
	d = appendLocalOrConst(d, key)
	d = appendLocalOrConst(d, value)

	if l != len(d) {
		panic(fmt.Sprintf("objectInsert %d %d", l, len(d)))
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

//go:inline
func (o objectInsert) Key() LocalOrConst {
	return getLocalOrConst(o, 12)
}

//go:inline
func (o objectInsert) Value() LocalOrConst {
	return getLocalOrConst(o, 16)
}

//go:inline
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
		panic(fmt.Sprintf("objectMerge %d %d", l, len(d)))
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

//go:inline
func (o objectMerge) A() Local {
	return getLocal(o, 8)
}

//go:inline
func (o objectMerge) B() Local {
	return getLocal(o, 12)
}

//go:inline
func (o objectMerge) Target() Local {
	return getLocal(o, 16)
}

func (nop) Write() []byte {
	l := 4 + 4
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementNop)

	if l != len(d) {
		panic(fmt.Sprintf("nop %d %d", l, len(d)))
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
		panic(fmt.Sprintf("not %d %d", l, len(d)))
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

//go:inline
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
		panic(fmt.Sprintf("resetLocal %d %d", l, len(d)))
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

//go:inline
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
		panic(fmt.Sprintf("resultsSetAdd %d %d", l, len(d)))
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

//go:inline
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
		panic(fmt.Sprintf("returnLocal %d %d", l, len(d)))
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

//go:inline
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
		panic(fmt.Sprintf("scan %d %d", l, len(d)))
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

//go:inline
func (s scan) Source() Local {
	return getLocal(s, 8)
}

//go:inline
func (s scan) Key() Local {
	return getLocal(s, 12)
}

//go:inline
func (s scan) Value() Local {
	return getLocal(s, 16)
}

//go:inline
func (s scan) Block() block {
	return block(s[20:])
}

func (with) Write(local Local, path []int, value LocalOrConst, block []byte) []byte {
	l := 4 + 4 + 4 + 4 + appendIntArraySize(path) + len(block)
	d := make([]byte, 0, l)
	d = appendUint32(d, 0) // Length placeholder.
	d = appendUint32(d, typeStatementWith)
	d = appendLocal(d, local)
	d = appendLocalOrConst(d, value)
	d = appendIntArray(d, path)
	d = append(d, block...)

	if l != len(d) {
		panic(fmt.Sprintf("with %d %d", l, len(d)))
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
	offset := uint32(16)
	n := getUint32(w, offset)

	a := make([]int, 0, n)
	for i := uint32(0); i < n; i++ {
		v := getInt32(w, offset+4+i*4)
		a = append(a, int(v))
	}
	return a
}

func (w with) PathLen() uint32 {
	offset := uint32(16)
	return getUint32(w, offset)
}

func (w with) PathIter(fcn func(i uint32, arg int) error) error {
	offset := uint32(16)
	n := getUint32(w, offset)

	for i := uint32(0); i < n; i++ {
		if err := fcn(i, int(getInt32(w, offset+4+i*4))); err != nil {
			return err
		}
	}
	return nil
}

func (w with) Value() LocalOrConst {
	return getLocalOrConst(w, 12)
}

func (w with) Block() block {
	n := getInt32ArraySize(w, 16)
	return block(w[12+int(n)+4:])
}

// Primitive types

func appendBool(d []byte, value bool) []byte {
	if value {
		return append(d, byte(1))
	}

	return append(d, byte(0))
}

//go:inline
func getBool(data []byte, offset uint32) bool {
	return data[offset] != 0
}

//go:inline
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

//go:inline
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

//go:inline
func getInt32(data []byte, offset uint32) int32 {
	return int32(binary.BigEndian.Uint32(data[offset:]))
}

func appendInt32(d []byte, value int32) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(value))
	return append(d, data...)
}

//go:inline
func getInt64(data []byte, offset uint32) int64 {
	return int64(binary.BigEndian.Uint64(data[offset:]))
}

func appendInt64(d []byte, value int64) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, uint64(value))
	return append(d, data...)
}

//go:inline
func getInt32ArrayLen(data []byte, offset uint32) int {
	n := getUint32(data, offset)
	return int(n)
}

//go:inline
func getInt32ArraySize(data []byte, offset uint32) int {
	return 4 + 4*getInt32ArrayLen(data, offset)
}

//go:inline
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

//go:inline
func getLen(data []byte) uint32 {
	return getUint32(data, 0)
}

//go:inline
func getType(data []byte) uint32 {
	return getUint32(data, 4)
}

func appendLocal(d []byte, local Local) []byte {
	return appendUint32(d, uint32(local))
}

//go:inline
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

//go:inline
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
	var t, w uint32

	switch v := lc.(type) {
	case Local:
		t = localType
		w = uint32(v)

	case BoolConst:
		t = boolConstType
		if v {
			w = 1
		}

	case StringIndexConst:
		t = stringIndexConstType
		w = uint32(v)

	default:
		panic("unsupported local or const")
	}

	return appendUint32(d, t<<24|w)
}

func getLocalOrConst(data []byte, offset uint32) LocalOrConst {
	v := getUint32(data, offset)
	t := v >> 24
	v = v & 0xffffff

	switch t {
	case localType:
		return Local(v)

	case boolConstType:
		if v == 0 {
			return BoolConst(false)
		}
		return BoolConst(true)

	case stringIndexConstType:
		return StringIndexConst(v)

	default:
		panic("unsupported local or const")
	}
}

func getLocalOrConstArray(data []byte, offset uint32) []LocalOrConst {
	n := getUint32(data, offset)

	l := make([]LocalOrConst, 0, n)
	for i := uint32(0); i < n; i++ {
		l = append(l, getLocalOrConst(data, offset+4+i*4))
	}

	return l
}

//go:inline
func getLocalOrConstArrayLen(data []byte, offset uint32) int {
	n := getUint32(data, offset)
	return int(n)
}

//go:inline
func getLocalOrConstArraySize(data []byte, offset uint32) int {
	return 4 + 4*getLocalOrConstArrayLen(data, offset)
}

func appendLocalOrConstArraySize(l []LocalOrConst) int {
	return 4 + 4*len(l)
}

func appendLocalOrConstArray(d []byte, l []LocalOrConst) []byte {
	d = appendUint32(d, uint32(len(l)))
	for _, l := range l {
		d = appendLocalOrConst(d, l)
	}

	return d
}
