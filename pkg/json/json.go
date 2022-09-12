package json

import (
	"bytes"
	"encoding"
	gojson "encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	internal "github.com/StyraInc/load/pkg/json/internal/json"
	"github.com/StyraInc/load/pkg/json/internal/utils"

	"github.com/open-policy-agent/opa/ast"
)

// Json is the interface every element within the document implements.
type Json interface {
	File
	fmt.Stringer

	// JSON returns Go JSON object. This is same as File.Contents().
	JSON() interface{}

	// AST return OPA ast.Value.
	AST() ast.Value

	// Extract returns the JSON element the provided JSON pointer (RFC 6901) refers to. The function returns an error if no element found.
	Extract(ptr string) (Json, error)

	// extract is the internal implementation of the Extract.
	extractImpl(ptr []string) (Json, error)

	// Find finds all JSON elements matching the JSON search path provided. It invokes the finder callback for each of the element found.
	Find(search Path, finder Finder)

	// Compare compares this JSON node ('a') to another JSON ('b'), returning -1, 0, 1 if 'a' is less than 'b', 'a' equals to 'b', or 'a' is more than 'b', respectively.
	Compare(other Json) int

	// Walk traverses the JSON document recursively, reporting each element to walker.
	Walk(state *DecodingState, walker Walker)
}

var (
	nullJson               = Null{}
	trueJson               = Bool{true}
	falseJson              = Bool{false}
	nullBytes              = []byte("null")
	trueBytes              = []byte("true")
	falseBytes             = []byte("false")
	leftBracketBytes       = []byte("[")
	rightBracketBytes      = []byte("]")
	leftCurlyBracketBytes  = []byte("{")
	rightCurlyBracketBytes = []byte("}")
	commaBytes             = []byte(",")
	colonBytes             = []byte(":")
)

// Iterable is implemented by both Arrays and Objects.
type Iterable interface {
	Len() int
	Iterate(i int) Json
	RemoveIdx(i int)
	SetIdx(i int, j File)
}

type Finder func(value Json)

func corrupted(err error) {
	if err != nil {
		panic(fmt.Sprintf("json: corrupted binary: %v", err))
	} else {
		panic("json: corrupted binary")
	}
}

func checkError(err error) {
	if err != nil {
		corrupted(err)
	}
}

// Null represents a JSON nil value.
type Null struct{}

func NewNull() Null {
	return nullJson
}

func IsNull(value Json) bool {
	return value == nullJson
}

func (n Null) WriteTo(w io.Writer) (int64, error) {
	written, err := w.Write(nullBytes)
	return int64(written), err
}

func (n Null) Contents() interface{} {
	return n.JSON()
}

func (n Null) JSON() interface{} {
	return nil
}

func (n Null) AST() ast.Value {
	return ast.Null{}
}

func (n Null) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return n.extractImpl(p)
}

func (n Null) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return n, nil
	}

	return nil, fmt.Errorf("json: path not found")
}

func (n Null) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, n, finder)
}

func (n Null) Compare(other Json) int {
	return compare(n, other)
}

func (n Null) Walk(state *DecodingState, walker Walker) {
	walker.Nil(state)
}

func (n Null) Clone(deepCopy bool) File {
	return n
}

func (n Null) String() string {
	return "null"
}

// Bool represents a JSON boolean value.
type Bool struct {
	value bool
}

func NewBool(value bool) Bool {
	if value {
		return trueJson
	}
	return falseJson
}

func (b Bool) WriteTo(w io.Writer) (int64, error) {
	var value []byte
	if b.value {
		value = trueBytes
	} else {
		value = falseBytes
	}

	written, err := w.Write(value)
	return int64(written), err
}

func (b Bool) Contents() interface{} {
	return b.JSON()
}

func (b Bool) Value() bool {
	return b.value
}

func (b Bool) JSON() interface{} {
	return b.Value()
}

func (b Bool) AST() ast.Value {
	return ast.Boolean(b.value)
}

func (b Bool) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return b.extractImpl(p)
}

func (b Bool) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return b, nil
	}

	return nil, fmt.Errorf("json: path not found")
}

func (b Bool) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, b, finder)
}

func (b Bool) Compare(other Json) int {
	return compare(b, other)
}

func (b Bool) Walk(state *DecodingState, walker Walker) {
	walker.Boolean(state, b)
}

func (b Bool) Clone(deepCopy bool) File {
	return b
}

func (b Bool) String() string {
	return fmt.Sprint(b.Value())
}

// Float represents a JSON float value.
type Float struct {
	value gojson.Number
}

func NewFloat(value gojson.Number) Float {
	return Float{value}
}

func NewFloatInt(i int64) Float {
	return NewFloat(gojson.Number(fmt.Sprintf("%d", i)))
}

func newFloat(content contentReader, offset int64) Float {
	value, err := content.ReadString(offset)
	checkError(err)

	return Float{value: gojson.Number(value)}
}

func (f Float) WriteTo(w io.Writer) (int64, error) {
	return writeStringJSON(w, string(f.value), false)
}

func (f Float) Contents() interface{} {
	return f.JSON()
}

func (f Float) Value() gojson.Number {
	return f.value
}

func (f Float) JSON() interface{} {
	return f.Value()
}

func (f Float) AST() ast.Value {
	return ast.Number(f.value)
}

func (f Float) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return f.extractImpl(p)
}

func (f Float) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return f, nil
	}

	return nil, fmt.Errorf("json: path not found")
}

func (f Float) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, f, finder)
}

func (f Float) Compare(other Json) int {
	return compare(f, other)
}

func (f Float) Walk(state *DecodingState, walker Walker) {
	walker.Number(state, f)
}

func (f Float) Clone(deepCopy bool) File {
	return f
}

func (f Float) String() string {
	return f.Value().String()
}

func (f Float) Add(addition Float) Float {
	ia, oka := f.value.Int64()
	ib, okb := addition.value.Int64()

	if oka == nil && okb == nil {
		return NewFloat(gojson.Number(fmt.Sprintf("%d", ia+ib)))
	}

	fa, oka := f.value.Float64()
	fb, okb := addition.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	return NewFloat(gojson.Number(fmt.Sprintf("%g", fa+fb)))
}

func (f Float) Sub(decrement Float) Float {
	ia, oka := f.value.Int64()
	ib, okb := decrement.value.Int64()

	if oka == nil && okb == nil {
		return NewFloat(gojson.Number(fmt.Sprintf("%d", ia-ib)))
	}

	fa, oka := f.value.Float64()
	fb, okb := decrement.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	return NewFloat(gojson.Number(fmt.Sprintf("%g", fa-fb)))
}

func (f Float) Multiply(multiplier Float) Float {
	ia, oka := f.value.Int64()
	ib, okb := multiplier.value.Int64()

	if oka == nil && okb == nil {
		return NewFloat(gojson.Number(fmt.Sprintf("%d", ia*ib)))
	}

	fa, oka := f.value.Float64()
	fb, okb := multiplier.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	return NewFloat(gojson.Number(fmt.Sprintf("%g", fa*fb)))
}

func (f Float) Divide(divisor Float) Float {
	fa, oka := f.value.Float64()
	fb, okb := divisor.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	// TODO: what if the result is an exact integer.

	return NewFloat(gojson.Number(fmt.Sprintf("%g", fa/fb)))
}

func Compare(a, b Float) int {
	ia, oka := a.value.Int64()
	ib, okb := b.value.Int64()

	if oka == nil && okb == nil {
		switch {
		case ia < ib:
			return -1
		case ia == ib:
			return 0
		case ia > ib:
			return 1
		}
	}

	fa, oka := a.value.Float64()
	fb, okb := b.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	switch {
	case fa < fb:
		return -1
	case fa == fb:
		return 0
	default:
		return 1
	}
}

func Min(a, b Float) Float {
	ia, oka := a.value.Int64()
	ib, okb := b.value.Int64()

	if oka == nil && okb == nil {
		if ia < ib {
			return NewFloat(gojson.Number(fmt.Sprintf("%d", ia)))
		} else {
			return NewFloat(gojson.Number(fmt.Sprintf("%d", ib)))
		}
	}

	fa, oka := a.value.Float64()
	fb, okb := b.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	if fa < fb {
		return NewFloat(gojson.Number(fmt.Sprintf("%g", fa)))
	} else {
		return NewFloat(gojson.Number(fmt.Sprintf("%g", fb)))
	}
}

func Max(a, b Float) Float {
	ia, oka := a.value.Int64()
	ib, okb := b.value.Int64()

	if oka == nil && okb == nil {
		if ia > ib {
			return NewFloat(gojson.Number(fmt.Sprintf("%d", ia)))
		} else {
			return NewFloat(gojson.Number(fmt.Sprintf("%d", ib)))
		}
	}

	fa, oka := a.value.Float64()
	fb, okb := b.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	if fa > fb {
		return NewFloat(gojson.Number(fmt.Sprintf("%g", fa)))
	} else {
		return NewFloat(gojson.Number(fmt.Sprintf("%g", fb)))
	}
}

// String represents a JSON string.
type String struct {
	str string
}

func NewString(s string) String {
	return String{s}
}

func newString(content contentReader, offset int64) String {
	str, err := content.ReadString(offset)
	checkError(err)
	return String{str: str}
}

func newStringInt(content contentReader, offset int64) String {
	n, err := content.ReadVarInt(offset)
	checkError(err)
	return String{fmt.Sprintf("%d", n)}
}

func (s String) WriteTo(w io.Writer) (int64, error) {
	return writeStringJSON(w, s.str, true)
}

func (s String) Contents() interface{} {
	return s.JSON()
}

func (s String) Value() string {
	return s.str
}

func (s String) JSON() interface{} {
	return s.Value()
}

func (s String) AST() ast.Value {
	return ast.String(s.str)
}

func (s String) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return s.extractImpl(p)
}

func (s String) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return s, nil
	}

	return nil, fmt.Errorf("json: path not found")
}

func (s String) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, s, finder)
}

func (s String) Compare(other Json) int {
	return compare(s, other)
}

func (s String) Walk(state *DecodingState, walker Walker) {
	walker.String(state, s)
}

func (s String) Clone(deepCopy bool) File {
	return s
}

func (s String) String() string {
	return strconv.Quote(s.Value())
}

// Array represents a JSON array.
type Array interface {
	Json
	Iterable

	Append(element ...File)
	Slice(i, j int) Array
	Value(i int) Json
	WriteI(w io.Writer, i int, written *int64) error
}

type ArrayBinary struct {
	content arrayReader
}

func newArray(reader contentReader, offset int64) Array {
	content, err := reader.ReadArray(offset)
	checkError(err)
	return ArrayBinary{content: content}
}

func (a ArrayBinary) WriteTo(w io.Writer) (int64, error) {
	return writeArrayJSON(w, a)
}

func (a ArrayBinary) Contents() interface{} {
	return a.JSON()
}

func (a ArrayBinary) Append(element ...File) {
	panic("json: unsupported append")
}

func (a ArrayBinary) Slice(i, j int) Array {
	// XXX
	panic("json: unsupported slice")
}

func (a ArrayBinary) Len() int {
	l, err := a.content.ArrayLen()
	checkError(err)
	return l
}

func (a ArrayBinary) Value(i int) Json {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	j := a.valueImpl(i)
	if v, ok := j.(Json); ok {
		return v
	} else {
		return nil
	}
}

func (a ArrayBinary) valueImpl(i int) File {
	offset, err := a.content.ArrayValueOffset(i)
	checkError(err)

	return newFile(a.content, offset)
}

func (a ArrayBinary) WriteI(w io.Writer, i int, written *int64) error {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	offset, err := a.content.ArrayValueOffset(i)
	checkError(err)

	return writeFile(a.content, offset, w, written)
}

func (a ArrayBinary) Iterate(i int) Json {
	return a.Value(i)
}

func (a ArrayBinary) RemoveIdx(i int) {
	panic("json: unsupported remove")
}

func (a ArrayBinary) SetIdx(i int, j File) {
	panic("json: unsupported set")
}

func (a ArrayBinary) JSON() interface{} {
	array := make([]interface{}, a.Len())
	for i := 0; i < a.Len(); i++ {
		array[i] = a.Value(i).JSON()
	}

	return array
}

func (a ArrayBinary) AST() ast.Value {
	array := make([]*ast.Term, a.Len())
	for i := 0; i < a.Len(); i++ {
		array[i] = ast.NewTerm(a.Value(i).AST())
	}

	return ast.NewArray(array...)
}

func (a ArrayBinary) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return a.extractImpl(p)
}

func (a ArrayBinary) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return a, nil
	}

	i, err := parseInt(ptr[0])
	if err != nil {
		return nil, fmt.Errorf("json: path not found")
	}

	if i < 0 || i >= a.Len() {
		return nil, fmt.Errorf("json: path not found")
	}

	return a.Value(i).extractImpl(ptr[1:])
}

func (a ArrayBinary) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, a, finder)
}

func (a ArrayBinary) Compare(other Json) int {
	return compare(a, other)
}

func (a ArrayBinary) Walk(state *DecodingState, walker Walker) {
	arrayWalk(a, state, walker)
}

func (a ArrayBinary) Clone(deepCopy bool) File {
	return a
}

func (a ArrayBinary) String() string {
	s := make([]string, a.Len())
	for i := 0; i < len(s); i++ {
		s[i] = a.Value(i).String()
	}
	return "[" + strings.Join(s, ",") + "]"
}

type ArraySlice struct {
	elements []File
}

func NewArray(elements ...File) Array {
	return &ArraySlice{elements: elements}
}

func (a *ArraySlice) WriteTo(w io.Writer) (int64, error) {
	return writeArrayJSON(w, a)
}

func (a *ArraySlice) Contents() interface{} {
	return a.JSON()
}

func (a *ArraySlice) Append(elements ...File) {
	a.elements = append(a.elements, elements...)
}

func (a *ArraySlice) Slice(i, j int) Array {
	return NewArray(a.elements[i:j]...)
}

func (a *ArraySlice) Len() int {
	return len(a.elements)
}

func (a *ArraySlice) Value(i int) Json {
	if i < 0 || i >= len(a.elements) {
		panic("json: index out of range")
	}

	if v, ok := a.elements[i].(Json); ok {
		return v
	} else {
		return nil
	}
}

func (a *ArraySlice) WriteI(w io.Writer, i int, written *int64) error {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	n, err := a.elements[i].WriteTo(w)
	*written += int64(n)
	return err
}

func (a *ArraySlice) Iterate(i int) Json {
	return a.Value(i)
}

func (a *ArraySlice) RemoveIdx(i int) {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	copy(a.elements[i:], a.elements[i+1:])
	a.elements[len(a.elements)-1] = nil // prevent the internal array to hold a ref to the deleted element
	a.elements = a.elements[:len(a.elements)-1]
}

func (a *ArraySlice) SetIdx(i int, j File) {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	a.elements[i] = j
}

func (a *ArraySlice) JSON() interface{} {
	array := make([]interface{}, len(a.elements))
	for i, e := range a.elements {
		j, ok := e.(Json)
		if ok {
			array[i] = j.JSON()
		}
	}

	return array
}

func (a ArraySlice) AST() ast.Value {
	array := make([]*ast.Term, len(a.elements))
	for i, e := range a.elements {
		j, ok := e.(Json)
		if ok {
			array[i] = ast.NewTerm(j.AST())
		}
	}

	return ast.NewArray(array...)
}

func (a *ArraySlice) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return a.extractImpl(p)
}

func (a *ArraySlice) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return a, nil
	}

	i, err := parseInt(ptr[0])
	if err != nil {
		return nil, fmt.Errorf("json: path not found")
	}

	if i < 0 || i >= a.Len() {
		return nil, fmt.Errorf("json: path not found")
	}

	return a.Value(i).extractImpl(ptr[1:])
}

func (a *ArraySlice) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, a, finder)
}

func (a *ArraySlice) Compare(other Json) int {
	return compare(a, other)
}

func (a *ArraySlice) Walk(state *DecodingState, walker Walker) {
	arrayWalk(a, state, walker)
}

func (a *ArraySlice) Clone(deepCopy bool) File {
	j := make([]File, a.Len())
	for i := 0; i < len(j); i++ {
		v := a.elements[i]
		if deepCopy {
			v = v.Clone(true).(Json)
		}
		j[i] = v
	}

	return NewArray(j...)
}

func (a *ArraySlice) String() string {
	s := make([]string, a.Len())
	for i := 0; i < len(s); i++ {
		s[i] = a.Value(i).String()
	}
	return "[" + strings.Join(s, ",") + "]"
}

// Object represents JSON object.
type Object interface {
	Json

	Names() []string
	Set(name string, value Json)
	setImpl(name string, value File)
	Value(name string) Json
	valueImpl(name string) File
	Remove(name string)
	Serialize(cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error)

	Iterable
}

type ObjectBinary struct {
	content objectReader
}

func newObject(reader contentReader, offset int64) ObjectBinary {
	content, err := reader.ReadObject(offset)
	checkError(err)
	return ObjectBinary{content: content}
}

func (o ObjectBinary) WriteTo(w io.Writer) (int64, error) {
	var written int64
	if err := writeSafe(w, leftCurlyBracketBytes, &written); err != nil {
		return written, err
	}

	properties, offsets, err := o.content.objectNameValueOffsets()
	checkError(err)

	for i := 0; i < len(properties); i++ {
		if err := writeValueSeparator(w, i, &written); err != nil {
			return written, err
		}

		if data, err := marshalStringJSON(properties[i].name, true); err != nil {
			return written, err
		} else if err := writeSafe(w, data, &written); err != nil {
			return written, err
		}

		if err := writeSafe(w, colonBytes, &written); err != nil {
			return written, err
		}

		if err := writeFile(o.content, offsets[i], w, &written); err != nil {
			return written, err
		}
	}

	err = writeSafe(w, rightCurlyBracketBytes, &written)
	return written, err
}

func (o ObjectBinary) Contents() interface{} {
	return o.JSON()
}

func (o ObjectBinary) Names() []string {
	names, err := o.content.ObjectNames()
	checkError(err)

	return names
}

func (o ObjectBinary) Set(name string, value Json) {
	o.setImpl(name, value)
}

func (o ObjectBinary) setImpl(name string, value File) {
	panic("json: unsupported set")
}

func (o ObjectBinary) Value(name string) Json {
	j := o.valueImpl(name)
	if j == nil {
		return nil
	}

	if _, ok := j.(Json); ok {
		return j.(Json)
	} else {
		return nil
	}
}

func (o ObjectBinary) valueImpl(name string) File {
	offset, ok, err := o.content.ObjectValueOffset(name)
	checkError(err)

	if !ok {
		return nil
	}

	return newFile(o.content, offset)
}

func (o ObjectBinary) Iterate(i int) Json {
	names := o.Names()
	return o.Value(names[i])
}

func (o ObjectBinary) RemoveIdx(i int) {
	panic("json: unsupported remove")
}

func (o ObjectBinary) SetIdx(i int, j File) {
	panic("json: unsupported set")
}

func (o ObjectBinary) Remove(name string) {
	panic("json: unsupported remove")
}

func (o ObjectBinary) Len() int {
	return o.content.ObjectLen()
}

func (o ObjectBinary) JSON() interface{} {
	properties, offsets, err := o.content.objectNameValueOffsets()
	object := make(map[string]interface{}, len(properties))
	checkError(err)

	for i := range properties {
		j, ok := newFile(o.content, offsets[i]).(Json)
		if ok {
			object[properties[i].name] = j.JSON()
		}
	}

	return object
}

func (o ObjectBinary) AST() ast.Value {
	properties, offsets, err := o.content.objectNameValueOffsets()
	object := make([][2]*ast.Term, 0, len(properties))
	checkError(err)

	for i := range properties {
		k := ast.String(properties[i].name)
		j, ok := newFile(o.content, offsets[i]).(Json)
		if ok {
			v := j.AST()
			object = append(object, [2]*ast.Term{ast.NewTerm(k), ast.NewTerm(v)})
		}
	}

	return ast.NewObject(object...)
}

func (o ObjectBinary) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return o.extractImpl(p)
}

func (o ObjectBinary) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return o, nil
	}

	v := o.Value(ptr[0])
	if v == nil {
		return nil, fmt.Errorf("json: path not found")
	}

	return v.extractImpl(ptr[1:])
}

func (o ObjectBinary) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, o, finder)
}

func (o ObjectBinary) Compare(other Json) int {
	return compare(o, other)
}

func (o ObjectBinary) Walk(state *DecodingState, walker Walker) {
	objectWalk(o, state, walker)
}

func (o ObjectBinary) Clone(deepCopy bool) File {
	return o
}

func (o ObjectBinary) String() string {
	var s []string

	properties, offsets, err := o.content.objectNameValueOffsets()
	checkError(err)

	for i, property := range properties {
		f := newFile(o.content, offsets[i])
		j, _ := f.(Json) // Non-JSON is printed as nil.
		s = append(s, fmt.Sprint(strconv.Quote(property.name), ":", j))
	}

	return "{" + strings.Join(s, ",") + "}"
}

func (o ObjectBinary) Serialize(cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	properties, offsets, err := o.content.objectNameValueOffsets()
	checkError(err)

	for i, offset := range offsets {
		properties[i].value = newFile(o.content, offset)
	}

	return serializeObject(properties, cache, buffer, base)
}

type ObjectMap struct {
	properties []objectEntry
}

type objectEntry struct {
	name  string
	value interface{}
}

func NewObject(properties map[string]File) Object {
	p := make([]objectEntry, 0, len(properties))

	for name, property := range properties {
		p = append(p, objectEntry{name, property})
	}

	sort.Slice(p, func(i, j int) bool { return p[i].name < p[j].name })

	return &ObjectMap{p}
}

func (o *ObjectMap) WriteTo(w io.Writer) (int64, error) {
	var written int64
	if err := writeSafe(w, leftCurlyBracketBytes, &written); err != nil {
		return written, err
	}

	for i := range o.properties {
		if err := writeValueSeparator(w, i, &written); err != nil {
			return written, err
		}

		if data, err := marshalStringJSON(o.properties[i].name, true); err != nil {
			return written, err
		} else if err := writeSafe(w, data, &written); err != nil {
			return written, err
		}

		if err := writeSafe(w, colonBytes, &written); err != nil {
			return written, err
		}

		if err := writeToSafe(w, o.properties[i].value.(File), &written); err != nil {
			return written, err
		}
	}

	err := writeSafe(w, rightCurlyBracketBytes, &written)
	return written, err
}

func (o *ObjectMap) Contents() interface{} {
	return o.JSON()
}

func (o *ObjectMap) Names() []string {
	names := make([]string, len(o.properties))
	for i := range o.properties {
		names[i] = o.properties[i].name
	}

	return names
}

func (o *ObjectMap) Set(name string, value Json) {
	o.setImpl(name, value)
}

func (o *ObjectMap) setImpl(name string, value File) {
	i, ok := o.find(name)
	if ok {
		o.properties[i] = objectEntry{name, value}
		return
	}

	properties := make([]objectEntry, len(o.properties)+1)
	copy(properties, o.properties[0:i])
	properties[i] = objectEntry{name, value}
	copy(properties[i+1:], o.properties[i:])
	o.properties = properties
}

func (o *ObjectMap) Value(name string) Json {
	j := o.valueImpl(name)
	if j == nil {
		return nil
	}

	if _, ok := j.(Json); ok {
		return j.(Json)
	}

	return nil
}

func (o *ObjectMap) find(name string) (int, bool) {
	// golang sort.Search implementation embedded here to assist compiler in inlining.
	i, j := 0, len(o.properties)
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j

		// begin f(h):
		ret := o.properties[h].name >= name
		// end f(h)

		if !ret {
			i = h + 1 // preserves f(i-1) == false
		} else {
			j = h // preserves f(j) == true
		}
	}

	return i, i < len(o.properties) && o.properties[i].name == name
}

func (o *ObjectMap) valueImpl(name string) File {
	i, ok := o.find(name)
	if !ok {
		return nil
	}

	return o.properties[i].value.(File)
}

func (o *ObjectMap) Iterate(i int) Json {
	return o.properties[i].value.(Json)
}

func (o *ObjectMap) RemoveIdx(i int) {
	if i < 0 || i >= len(o.properties) {
		panic("json: index out of range")
	}

	properties := make([]objectEntry, len(o.properties)-1)
	copy(properties, o.properties[0:i])
	copy(properties[i:], o.properties[i+1:])
	o.properties = properties
}

func (o *ObjectMap) SetIdx(i int, j File) {
	if i < 0 || i >= len(o.properties) {
		panic("json: index out of range")
	}

	o.properties[i].value = j
}

func (o *ObjectMap) Remove(name string) {
	if i, ok := o.find(name); ok {
		o.RemoveIdx(i)
	}
}

func (o *ObjectMap) Len() int {
	return len(o.properties)
}

func (o *ObjectMap) JSON() interface{} {
	object := make(map[string]interface{}, len(o.properties))
	for i := range o.properties {
		j, ok := o.properties[i].value.(Json)
		if ok {
			object[o.properties[i].name] = j.JSON()
		}
		// else: TODO: Should set the value to nil, to be identical with array processing?
	}

	return object
}

func (o ObjectMap) AST() ast.Value {
	object := make([][2]*ast.Term, 0, len(o.properties))

	for i := range o.properties {
		j, ok := o.properties[i].value.(Json)
		if ok {
			k := ast.String(o.properties[i].name)
			object = append(object, [2]*ast.Term{ast.NewTerm(k), ast.NewTerm(j.AST())})
		}
		// else: TODO: Should set the value to nil, to be identical with array processing?
	}

	return ast.NewObject(object...)
}

func (o *ObjectMap) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return o.extractImpl(p)
}

func (o *ObjectMap) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return o, nil
	}

	v := o.Value(ptr[0])
	if v == nil {
		return nil, fmt.Errorf("json: path not found")
	}

	return v.extractImpl(ptr[1:])
}

func (o *ObjectMap) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, o, finder)
}

func (o *ObjectMap) Compare(other Json) int {
	return compare(o, other)
}

func (o *ObjectMap) Walk(state *DecodingState, walker Walker) {
	objectWalk(o, state, walker)
}

func (o *ObjectMap) Clone(deepCopy bool) File {
	properties := make([]objectEntry, 0, len(o.properties))
	for i := range o.properties {
		v := o.properties[i].value
		if deepCopy {
			v = v.(File).Clone(true)
		}
		properties = append(properties, objectEntry{o.properties[i].name, v})
	}

	return &ObjectMap{properties}
}

func (o *ObjectMap) String() string {
	var s []string

	for _, name := range o.Names() {
		s = append(s, fmt.Sprint(strconv.Quote(name), ":", o.Value(name)))
	}

	return "{" + strings.Join(s, ",") + "}"
}

func (o *ObjectMap) Serialize(cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	return serializeObject(o.properties, cache, buffer, base)
}

// compare compares two JSON values, returning -1, 0, 1 if a is less
// than b, a equals to b, or a is more than b, respectively.
func compare(x Json, y Json) int {
	ka, kb := jsonType(x), jsonType(y)

	if ka == kb {
		switch x.(type) {
		case Array:
			a, b := x.(Array), y.(Array)

			if a.Len() < b.Len() {
				return -1
			}

			if a.Len() > b.Len() {
				return 1
			}

			for i := 0; i < a.Len(); i++ {
				c := compare(a.Value(i), b.Value(i))
				if c != 0 {
					return c
				}
			}

			return 0

		case Object:
			a, b := x.(Object), y.(Object)

			if a.Len() < b.Len() {
				return -1
			}

			if a.Len() > b.Len() {
				return 1
			}

			keysa, keysb := a.Names(), b.Names()

			for i := 0; i < a.Len(); i++ {
				c := strings.Compare(keysa[i], keysb[i])
				if c != 0 {
					return c
				}

				aa := a.Value(keysa[i])
				bb := b.Value(keysa[i])
				if aa == nil || bb == nil {
					panic(fmt.Sprintf("json: compared value for key '%s' not found", keysa[i]))
				}
				c = compare(aa, bb)
				if c != 0 {
					return c
				}
			}

			return 0

		case Bool:
			ba, bb := x.(Bool).Value(), y.(Bool).Value()
			switch {
			case !ba && bb:
				return -1
			case ba == bb:
				return 0
			case ba && !bb:
				return 1
			}

		case Float:
			return Compare(x.(Float), y.(Float))

		case String:
			return strings.Compare(x.(String).Value(), y.(String).Value())

		case Null:
			return 0

		case Blob:
			return bytes.Compare(x.(Blob).Value(), y.(Blob).Value())

		default:
			panic("not reached")
		}
	}

	// Of different types, simply compare the types.
	switch {
	case ka < kb:
		return -1
	case ka > kb:
		return 1
	}

	panic("not reached")
}

// jsonType returns a unique number for each type. Note that the caller should not assume anything about the numbers but that they are unique for each JSON type and they
// can be used to order the types.
func jsonType(j Json) int {
	switch j.(type) {
	case Null:
		return typeNil
	case Bool:
		return typeFalse // typeTrue never returned.
	case String:
		return typeString // typeStringInt and typeStringNumber never returned.
	case Float:
		return typeNumber
	case Array:
		return typeArray
	case Object:
		return typeObjectFull // typeObjectThin nor typeObjectPatch never returned.
	case Blob:
		return typeBinaryFull
	}

	corrupted(nil)
	return -1
}

// find finds the JSON elements referred by the search path from the doc, invoking finder for each.
func find(search Path, path []byte, doc Json, finder Finder) {
	if len(search) == 0 {
		finder(doc)
		return
	}

	pl := len(path)

	switch j := doc.(type) {
	case Array:
		start := 0
		end := 0
		drop := 1

		if search[0].IsRecursive() {
			find(search[1:], path, j, finder)

			drop = 0
			end = j.Len()

		} else if search[0].IsArray() && search[0].Index() == -1 {
			// Wildcards (-1) indicates iteration over each array element
			end = j.Len()
		} else if search[0].IsObjectWildcard() {
			end = j.Len()
		} else if search[0].IsArray() {
			start = search[0].Index()
			end = start + 1
		}
		// else: Path does not match the document.

		path = append(path, []byte("[")...)

		for k := start; k < end && k < j.Len(); k++ {
			path = strconv.AppendInt(path, int64(k), 10)
			path = append(path, []byte("]")...)

			find(search[drop:], path, j.Value(k), finder)

			// Drop the the array index and ']' so
			// that the next round will correctly
			// again add the index and ']'.
			path = path[0 : pl+1]
		}

		return

	case Object:
		var keys []string
		drop := 1

		if search[0].IsRecursive() {
			find(search[1:], path, j, finder)

			keys = j.Names()
			drop = 0

		} else if search[0].IsObjectWildcard() {
			keys = j.Names()
		} else if key := search[0].Property(); search[0].IsObject() {
			v := j.Value(key)
			if v != nil {
				keys = []string{key}
			}
		}

		for _, key := range keys {
			v := j.Value(key)
			path = append(path, []byte(".")...)
			path = append(path, []byte(key)...)

			find(search[drop:], path, v, finder)

			path = path[0:pl]
		}

		return
	}

	// String, Float, Bool, Null: consume the recursive segment if one available.

	if search[0].IsRecursive() {
		find(search[1:], path, doc, finder)
	}
}

// New constructs a JSON object out of go native types. It supports the struct tags.
func New(value interface{}) (Json, error) {
	doc, err := unmarshal(reflect.ValueOf(value), reflect.TypeOf(value))
	if err != nil {
		return nil, fmt.Errorf("json: unable to encode to JSON: %w", err)
	}

	return doc, nil
}

// MustNew constructs a JSON object out of go native types. It supports the struct tags. It panics if the provided value does not convert to JSON.
func MustNew(value interface{}) Json {
	doc, err := New(value)
	if err != nil {
		panic("cannot build JSON")
	}

	return doc
}

// NewFromBinary reads a JSON snapshot.
func NewFromBinary(data []byte) (Json, error) {
	reader := utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(data))
	snapshot := newSnapshotReader(reader)
	t, err := snapshot.ReadType(0)
	if err != nil {
		return nil, err
	}
	switch t {
	case typeNil:
		return NewNull(), nil
	case typeFalse:
		return NewBool(false), nil
	case typeTrue:
		return NewBool(true), nil
	case typeString:
		return newString(snapshot, 0), nil
	case typeStringInt:
		return newStringInt(snapshot, 0), nil
	case typeNumber:
		return newFloat(snapshot, 0), nil
	case typeArray:
		return newArray(snapshot, 0), nil
	case typeObjectFull, typeObjectThin:
		return newObject(snapshot, 0), nil
	default:
		return nil, fmt.Errorf("unsupported type: %v", t)
	}
}

// Marshal serializes the JSON to a single binary snapshot.
func Marshal(j Json) ([]byte, error) {
	cache := newEncodingCache()
	buffer := new(bytes.Buffer)

	if _, err := serialize(j, cache, buffer, 0); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

var (
	numberType                = reflect.TypeOf(gojson.Number("0.0"))
	jsonMarshalType           = reflect.TypeOf((*gojson.Marshaler)(nil)).Elem()
	encodingTextMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	jsonJsonType              = reflect.TypeOf((*Json)(nil)).Elem()
)

// unmarshal takes the value read from, but also its type. This because the type might not be available from the value itself, if it's of invalid type -- whereas
// the caller might know it.
func unmarshal(value reflect.Value, typ reflect.Type) (Json, error) {
	// Handle the gojson.Marshaler encoding.MarshalText interfaces. Note, their implemented semantics in the golang json package
	// calls for writing JSON null, even if the implementation of the marshaller could handle writing a nil value.
	if typ != nil {
		if typ.Implements(jsonJsonType) {
			return value.Interface().(Json), nil
		}

		if typ.Implements(jsonMarshalType) {
			if value.Kind() == reflect.Ptr && value.IsNil() {
				return NewNull(), nil
			}

			raw, err := value.Interface().(gojson.Marshaler).MarshalJSON()
			if err != nil {
				return nil, err
			}
			return NewDecoder(bytes.NewReader(raw)).Decode()
		}

		if typ.Implements(encodingTextMarshalerType) {
			if value.Kind() == reflect.Ptr && value.IsNil() {
				return NewNull(), nil
			}

			marshaller := value.Interface().(encoding.TextMarshaler)
			raw, err := marshaller.MarshalText()
			if err != nil {
				return nil, err
			}
			return NewString(string(raw)), nil
		}
	}

	switch value.Kind() {
	case reflect.Invalid:
		return NewNull(), nil
	case reflect.Interface:
		if v := value.Elem(); v.IsValid() {
			return unmarshal(value.Elem(), value.Elem().Type())
		} else {
			return unmarshal(value.Elem(), nil)
		}
	case reflect.Ptr:
		return unmarshal(value.Elem(), typ.Elem())
	case reflect.Array, reflect.Slice:
		return unmarshalArray(value, value.Type().Elem())
	case reflect.Bool:
		return NewBool(value.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return NewFloatInt(value.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return NewFloat(gojson.Number(strconv.FormatUint(value.Uint(), 10))), nil
	case reflect.Float32, reflect.Float64:
		raw, err := gojson.Marshal(value.Interface())
		if err != nil {
			return nil, err
		}

		return NewFloat(gojson.Number(raw)), nil
	case reflect.Complex64, reflect.Complex128:
		return nil, fmt.Errorf("json: unsupported type: %v", value.Type())
	case reflect.String:
		if value.Type() == numberType {
			return NewFloat(gojson.Number(value.String())), nil
		}

		return NewString(value.String()), nil
	case reflect.Map:
		return unmarshalMap(value)
	case reflect.Struct:
		return unmarshalStruct(value)
	default:
		return nil, fmt.Errorf("json: unsupported type: %v", value.Type())
	}
}

func unmarshalArray(values reflect.Value, typ reflect.Type) (Json, error) {
	if values.Kind() == reflect.Slice && values.IsNil() {
		return NewNull(), nil
	}

	n := values.Len()
	a := make([]File, 0, n)

	for i := 0; i < n; i++ {
		var err error
		value, err := unmarshal(values.Index(i), typ)
		if err != nil {
			return nil, err
		}

		a = append(a, value)
	}

	return NewArray(a...), nil
}

func unmarshalMap(values reflect.Value) (Json, error) {
	if values.IsNil() {
		return NewNull(), nil
	}

	m := make(map[string]File)
	iter := values.MapRange()

	elem := values.Type().Elem()
	switch values.Type().Key().Kind() {
	case reflect.String:
		for iter.Next() {
			keyStr := iter.Key().String()

			v, err := unmarshal(iter.Value(), elem)
			if err != nil {
				return nil, err
			}

			m[keyStr] = v
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		for iter.Next() {
			keyStr := strconv.FormatInt(iter.Key().Int(), 10)

			v, err := unmarshal(iter.Value(), elem)
			if err != nil {
				return nil, err
			}

			m[keyStr] = v
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		for iter.Next() {
			keyStr := strconv.FormatUint(iter.Key().Uint(), 10)

			v, err := unmarshal(iter.Value(), elem)
			if err != nil {
				return nil, err
			}

			m[keyStr] = v
		}

	default:
		key := values.Type().Key()
		if !key.Implements(encodingTextMarshalerType) {
			return nil, fmt.Errorf("json: unsupported type %v", values.Type())
		}

		for iter.Next() {
			k := iter.Key()
			var keyStr string

			if k.Kind() == reflect.Ptr && k.IsNil() {
				// consider nil key as "" per golang 1.14, even if implements TextMarshaler.
			} else {
				raw, err := k.Interface().(encoding.TextMarshaler).MarshalText()
				if err != nil {
					return nil, err
				}

				keyStr = string(raw)
			}

			v, err := unmarshal(iter.Value(), elem)
			if err != nil {
				return nil, err
			}

			m[keyStr] = v
		}
	}

	return NewObject(m), nil
}

func unmarshalStruct(values reflect.Value) (Json, error) {
	fields := internal.CachedTypeFields(values.Type())
	m := make(map[string]File, len(fields))

	for _, f := range fields {
		fv := fieldByIndex(values, f.Index)
		if !fv.IsValid() || f.OmitEmpty && isEmptyValue(fv) {
			continue
		}

		v, err := unmarshal(fv, fv.Type())
		if err != nil {
			return nil, err
		}

		m[f.Name] = v
	}

	return NewObject(m), nil
}

// marshalStringJSON implements fast serialization of simple JSON strings. Everything but ASCII control characters (0-31), double quotes and backslash are safe, requiring no escaping.
func marshalStringJSON(s string, quotes bool) ([]byte, error) {
	// Try simple serialization but give up once Unicode or escaping stumbled onto.

	l := len(s)
	buf := make([]byte, 0, l+2)
	if quotes {
		buf = append(buf, '"')
	}

	i := 0
	for ; i < l; i++ {
		if c := s[i]; c > 31 && c < 128 && c != '"' && c != '\\' {
			buf = append(buf, c)
		} else {
			break
		}
	}

	if i == l {
		if quotes {
			return append(buf, '"'), nil
		} else {
			return buf, nil
		}
	}

	// Revert to full serialization.
	return gojson.Marshal(s)
}

func writeStringJSON(w io.Writer, s string, quotes bool) (int64, error) {
	data, err := marshalStringJSON(s, quotes)
	if err != nil {
		return 0, err
	}

	n, err := w.Write(data)
	return int64(n), err
}

func writeArrayJSON(w io.Writer, a Array) (int64, error) {
	var written int64
	if err := writeSafe(w, leftBracketBytes, &written); err != nil {
		return written, err
	}

	l := a.Len()
	for i := 0; i < l; i++ {
		if err := writeValueSeparator(w, i, &written); err != nil {
			return written, err
		}

		if err := a.WriteI(w, i, &written); err != nil {
			return written, err
		}
	}

	err := writeSafe(w, rightBracketBytes, &written)
	return written, err
}

func writeSafe(w io.Writer, data []byte, written *int64) error {
	n, err := w.Write(data)
	*written += int64(n)
	return err
}

func writeToSafe(w io.Writer, data io.WriterTo, written *int64) error {
	n, err := data.WriteTo(w)
	*written += n
	return err
}

func writeValueSeparator(w io.Writer, i int, written *int64) error {
	if i == 0 {
		return nil
	}

	return writeSafe(w, commaBytes, written)
}

func parseInt(str string) (int, error) {
	v, err := strconv.ParseInt(str, 10, 32)
	if err != nil {
		return 0, err
	}
	return int(v), nil
}
