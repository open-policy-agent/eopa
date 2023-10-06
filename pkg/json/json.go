package json

import (
	"bytes"
	"encoding"
	gojson "encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	internal "github.com/styrainc/enterprise-opa-private/pkg/json/internal/json"
	"github.com/styrainc/enterprise-opa-private/pkg/json/internal/utils"

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
	errPathNotFound        = errors.New("json: path not found")
)

// Iterable is implemented by both Arrays and Objects.
type Iterable interface {
	Len() int
	Iterate(i int) Json
	iterate(i int) File
	RemoveIdx(i int) Json
	SetIdx(i int, j File) Json
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
	return nil, errPathNotFound
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

func (n Null) Clone(bool) File {
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

	return nil, errPathNotFound
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

func (b Bool) Clone(bool) File {
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
	return NewFloat(gojson.Number(strconv.FormatInt(i, 10)))
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

	return nil, errPathNotFound
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

func (f Float) Clone(bool) File {
	return f
}

func (f Float) String() string {
	return f.Value().String()
}

func (f Float) Add(addition Float) Float {
	ia, oka := f.value.Int64()
	ib, okb := addition.value.Int64()

	if oka == nil && okb == nil {
		return NewFloat(gojson.Number(strconv.FormatInt(ia+ib, 10)))
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
		return NewFloat(gojson.Number(strconv.FormatInt(ia-ib, 10)))
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
		return NewFloat(gojson.Number(strconv.FormatInt(ia*ib, 10)))
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
			return NewFloat(gojson.Number(strconv.FormatInt(ia, 10)))
		}
		return NewFloat(gojson.Number(strconv.FormatInt(ib, 10)))
	}

	fa, oka := a.value.Float64()
	fb, okb := b.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	if fa < fb {
		return NewFloat(gojson.Number(fmt.Sprintf("%g", fa)))
	}
	return NewFloat(gojson.Number(fmt.Sprintf("%g", fb)))
}

func Max(a, b Float) Float {
	ia, oka := a.value.Int64()
	ib, okb := b.value.Int64()

	if oka == nil && okb == nil {
		if ia > ib {
			return NewFloat(gojson.Number(strconv.FormatInt(ia, 10)))
		}
		return NewFloat(gojson.Number(strconv.FormatInt(ib, 10)))
	}

	fa, oka := a.value.Float64()
	fb, okb := b.value.Float64()

	if oka != nil || okb != nil {
		panic("json: corrupted number")
	}

	if fa > fb {
		return NewFloat(gojson.Number(fmt.Sprintf("%g", fa)))
	}
	return NewFloat(gojson.Number(fmt.Sprintf("%g", fb)))
}

// String represents a JSON string.
type String string

func NewString(s string) *String {
	ss := String(s)
	return &ss
}

func newString(content contentReader, offset int64) *String {
	str, err := content.ReadString(offset)
	checkError(err)
	s := String(str)
	return &s
}

func newStringInt(content contentReader, offset int64) *String {
	n, err := content.ReadVarInt(offset)
	checkError(err)
	s := String(strconv.FormatInt(n, 10))
	return &s
}

func (s *String) WriteTo(w io.Writer) (int64, error) {
	return writeStringJSON(w, string(*s), true)
}

func (s *String) Contents() interface{} {
	return s.JSON()
}

func (s *String) Value() string {
	return string(*s)
}

func (s *String) JSON() interface{} {
	return s.Value()
}

func (s *String) AST() ast.Value {
	return ast.String(*s)
}

func (s *String) Extract(ptr string) (Json, error) {
	p, err := preparePointer(ptr)
	if err != nil {
		return nil, err
	}

	return s.extractImpl(p)
}

func (s *String) extractImpl(ptr []string) (Json, error) {
	if len(ptr) == 0 {
		return s, nil
	}

	return nil, errPathNotFound
}

func (s *String) Find(search Path, finder Finder) {
	find(search, []byte{'$'}, s, finder)
}

func (s *String) Compare(other Json) int {
	return compare(s, other)
}

func (s *String) Walk(state *DecodingState, walker Walker) {
	walker.String(state, *s)
}

func (s *String) Clone(bool) File {
	return s
}

func (s *String) String() string {
	return strconv.Quote(s.Value())
}

// Array represents a JSON array.
type Array interface {
	Json
	Iterable

	Append(element ...File) Array
	AppendSingle(element File) (Array, bool)
	Slice(i, j int) Array
	Value(i int) Json
	valueImpl(i int) File
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

func (a ArrayBinary) Append(elements ...File) Array {
	return a.clone().Append(elements...)
}

func (a ArrayBinary) AppendSingle(element File) (Array, bool) {
	n, _ := a.clone().AppendSingle(element)
	return n, true
}

func (a ArrayBinary) Slice(i int, j int) Array {
	return a.clone().Slice(i, j)
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
	}
	return nil
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

func (a ArrayBinary) iterate(i int) File {
	return a.valueImpl(i)
}

func (a ArrayBinary) RemoveIdx(i int) Json {
	return a.clone().RemoveIdx(i)
}

func (a ArrayBinary) SetIdx(i int, value File) Json {
	return a.clone().SetIdx(i, value)
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
		return nil, errPathNotFound
	}

	if i < 0 || i >= a.Len() {
		return nil, errPathNotFound
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

func (a ArrayBinary) Clone(bool) File {
	return a
}

func (a ArrayBinary) clone() Array {
	c := make([]File, a.Len())
	for i := 0; i < len(c); i++ {
		c[i] = a.valueImpl(i)
	}

	return newArrayImpl(c)
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
	return newArrayImpl(elements)
}

func NewArrayWithCapacity(capacity int) Array {
	if capacity == 0 {
		return &ArraySlice{nil}
	}

	return &ArraySlice{make([]File, 0, capacity)}
}

func newArrayImpl(elements []File) Array {
	if len(elements) == 0 {
		return &ArraySlice{nil}
	}

	return &ArraySlice{elements}
}

func (a *ArraySlice) WriteTo(w io.Writer) (int64, error) {
	return writeArrayJSON(w, a)
}

func (a *ArraySlice) Contents() interface{} {
	return a.JSON()
}

func (a *ArraySlice) Append(elements ...File) Array {
	a.elements = append(a.elements, elements...)
	return a
}

func (a *ArraySlice) AppendSingle(element File) (Array, bool) {
	a.elements = append(a.elements, element)
	return a, false
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
	}
	return nil
}

func (a *ArraySlice) valueImpl(i int) File {
	return a.elements[i]
}

func (a *ArraySlice) WriteI(w io.Writer, i int, written *int64) error {
	return arraySliceBase[*ArraySlice]{}.WriteI(a, w, i, written)
}

func (a *ArraySlice) Iterate(i int) Json {
	return a.Value(i)
}

func (a *ArraySlice) iterate(i int) File {
	return a.valueImpl(i)
}

func (a *ArraySlice) RemoveIdx(i int) Json {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	copy(a.elements[i:], a.elements[i+1:])
	a.elements[len(a.elements)-1] = nil // prevent the internal array to hold a ref to the deleted element
	a.elements = a.elements[:len(a.elements)-1]
	return a
}

func (a *ArraySlice) SetIdx(i int, j File) Json {
	if i < 0 || i >= a.Len() {
		panic("json: index out of range")
	}

	a.elements[i] = j
	return a
}

func (a *ArraySlice) JSON() interface{} {
	return arraySliceBase[*ArraySlice]{}.JSON(a)
}

func (a *ArraySlice) AST() ast.Value {
	return arraySliceBase[*ArraySlice]{}.AST(a)
}

func (a *ArraySlice) Extract(ptr string) (Json, error) {
	return arraySliceBase[*ArraySlice]{}.Extract(a, ptr)
}

func (a *ArraySlice) extractImpl(ptr []string) (Json, error) {
	return arraySliceBase[*ArraySlice]{}.extractImpl(a, ptr)
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
	return arraySliceBase[*ArraySlice]{}.clone(a, deepCopy)
}

func (a *ArraySlice) String() string {
	return arraySliceBase[*ArraySlice]{}.String(a)
}

// Object represents JSON object.
type Object interface {
	Json

	Names() []string
	Set(name string, value Json) (Object, bool)
	setImpl(name string, value File) (Object, bool)
	Value(name string) Json
	valueImpl(name string) File
	Remove(name string) Object
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

func (o ObjectBinary) NamesIndex(i int) string {
	name, err := o.content.ObjectNamesIndex(i)
	checkError(err)
	return name
}

func (o ObjectBinary) Set(name string, value Json) (Object, bool) {
	return o.setImpl(name, value)
}

func (o ObjectBinary) setImpl(name string, value File) (Object, bool) {
	n, _ := o.clone().setImpl(name, value)
	return n, true
}

func (o ObjectBinary) Value(name string) Json {
	j := o.valueImpl(name)
	if j == nil {
		return nil
	}

	if _, ok := j.(Json); ok {
		return j.(Json)
	}
	return nil
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
	return o.Value(o.NamesIndex(i))
}

func (o ObjectBinary) iterate(i int) File {
	return o.valueImpl(o.NamesIndex(i))
}

func (o ObjectBinary) RemoveIdx(i int) Json {
	return o.clone().RemoveIdx(i)
}

func (o ObjectBinary) SetIdx(i int, value File) Json {
	return o.clone().SetIdx(i, value)
}

func (o ObjectBinary) Remove(name string) Object {
	return o.clone().Remove(name)
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
		return nil, errPathNotFound
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

func (o ObjectBinary) Clone(bool) File {
	return o
}

func (o ObjectBinary) clone() Object {
	n := o.Len()
	p := make(map[string]File, n)
	for i := 0; i < n; i++ {
		name := o.NamesIndex(i)
		p[name] = o.valueImpl(name)
	}

	return NewObject(p)
}

func (o ObjectBinary) String() string {
	properties, offsets, err := o.content.objectNameValueOffsets()
	checkError(err)

	s := make([]string, 0, len(properties))
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
	internedKeys *[]string
	values       []interface{}
}

type objectEntry struct {
	value interface{}
	name  string
}

func NewObject(properties map[string]File) Object {
	return newObjectImpl(properties, nil)
}

func newObjectImpl(properties map[string]File, interning map[interface{}]*[]string) Object {
	keys := make([]string, len(properties))
	values := make([]interface{}, len(properties))

	i := 0
	for key, value := range properties {
		keys[i] = key
		values[i] = value
		i++
	}

	sort.Sort(&keyValueSlices{keys, values})

	obj := &ObjectMap{nil, values}
	obj.internedKeys = obj.intern(keys, interning)
	return obj
}

type keyValueSlices struct {
	keys   []string
	values []interface{}
}

func (kv *keyValueSlices) Len() int {
	return len(kv.keys)
}

func (kv *keyValueSlices) Less(i, j int) bool {
	return kv.keys[i] < kv.keys[j]
}

func (kv *keyValueSlices) Swap(i, j int) {
	kv.keys[i], kv.keys[j] = kv.keys[j], kv.keys[i]
	kv.values[i], kv.values[j] = kv.values[j], kv.values[i]
}

func (o *ObjectMap) WriteTo(w io.Writer) (int64, error) {
	return objectMapBase[*ObjectMap]{}.WriteTo(o, w)
}

func (o *ObjectMap) Contents() interface{} {
	return o.JSON()
}

func (o *ObjectMap) Names() []string {
	return o.keys()
}

func (o *ObjectMap) Set(name string, value Json) (Object, bool) {
	return o.setImpl(name, value)
}

func (o *ObjectMap) setImpl(name string, value File) (Object, bool) {
	i, ok := o.find(name)
	if ok {
		o.values[i] = value
		return o, false
	}

	curr := o.keys()
	keys := make([]string, len(curr)+1)
	copy(keys, curr[0:i])
	keys[i] = name
	copy(keys[i+1:], curr[i:])
	o.internedKeys = o.intern(keys, nil) // No interning.

	values := make([]interface{}, len(o.values)+1)
	copy(values, o.values[0:i])
	values[i] = value
	copy(values[i+1:], o.values[i:])
	o.values = values
	return o, false
}

func (o *ObjectMap) Value(name string) Json {
	return objectMapBase[*ObjectMap]{}.Value(o, name)
}

func (o *ObjectMap) find(name string) (int, bool) {
	return objectMapBase[*ObjectMap]{}.find(o.keys(), name)
}

func (o *ObjectMap) valueImpl(name string) File {
	i, ok := o.find(name)
	if !ok {
		return nil
	}

	return o.values[i].(File)
}

func (o *ObjectMap) Iterate(i int) Json {
	return o.values[i].(Json)
}

func (o *ObjectMap) iterate(i int) File {
	return o.values[i].(File)
}

func (o *ObjectMap) RemoveIdx(i int) Json {
	if i < 0 || i >= len(o.values) {
		panic("json: index out of range")
	}

	curr := o.keys()
	keys := make([]string, len(curr)-1)
	copy(keys, curr[0:i])
	copy(keys[i:], curr[i+1:])

	values := make([]interface{}, len(o.values)-1)
	copy(values, o.values[0:i])
	copy(values[i:], o.values[i+1:])

	return &ObjectMap{o.intern(keys, nil), values} // No interning.
}

func (o *ObjectMap) SetIdx(i int, j File) Json {
	keys := o.keys()
	if i < 0 || i >= len(keys) {
		panic("json: index out of range")
	}

	o.values[i] = j
	return o
}

func (o *ObjectMap) Remove(name string) Object {
	if i, ok := o.find(name); ok {
		return o.RemoveIdx(i).(Object)
	}

	return o
}

func (o *ObjectMap) Len() int {
	return len(o.values)
}

func (o *ObjectMap) JSON() interface{} {
	return objectMapBase[*ObjectMap]{}.JSON(o)
}

func (o *ObjectMap) AST() ast.Value {
	return objectMapBase[*ObjectMap]{}.AST(o)
}

func (o *ObjectMap) Extract(ptr string) (Json, error) {
	return objectMapBase[*ObjectMap]{}.Extract(o, ptr)
}

func (o *ObjectMap) extractImpl(ptr []string) (Json, error) {
	return objectMapBase[*ObjectMap]{}.extractImpl(o, ptr)
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
	return objectMapBase[*ObjectMap]{}.clone(o, o.internedKeys, deepCopy)
}

func (o *ObjectMap) String() string {
	return objectMapBase[*ObjectMap]{}.String(o)
}

func (o *ObjectMap) Serialize(cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	return objectMapBase[*ObjectMap]{}.Serialize(o, cache, buffer, base)
}

func (o *ObjectMap) intern(s []string, keys map[interface{}]*[]string) *[]string {
	return objectMapBase[*ObjectMap]{}.intern(s, keys)
}

func (o *ObjectMap) keys() []string {
	return *o.internedKeys
}

// compare compares two JSON values, returning -1, 0, 1 if a is less
// than b, a equals to b, or a is more than b, respectively.
func compare(x File, y File) int {
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
				c := compare(a.valueImpl(i), b.valueImpl(i))
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

				aa := a.valueImpl(keysa[i])
				bb := b.valueImpl(keysa[i])
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

		case *String:
			return strings.Compare(x.(*String).Value(), y.(*String).Value())

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
func jsonType(j File) int {
	switch j.(type) {
	case Null:
		return typeNil
	case Bool:
		return typeFalse // typeTrue never returned.
	case *String:
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

type unmarshaller struct {
	strings map[string]*String
	keys    map[interface{}]*[]string
}

func (u *unmarshaller) intern(v string) *String {
	return internString(v, u.strings)
}

// New constructs a JSON object out of go native types. It supports the struct tags.
func New(value interface{}) (Json, error) {
	u := unmarshaller{strings: make(map[string]*String), keys: make(map[interface{}]*[]string)}
	doc, err := u.unmarshal(reflect.ValueOf(value), reflect.TypeOf(value))
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

// IsBJson checks header: watch out for {/t,/n,/r} (valid json)
func IsBJson(data []byte) bool {
	t := data[0]
	switch t {
	case typeNil, typeFalse, typeTrue, typeString, typeStringInt, typeNumber, typeArray, typeObjectFull: // typeObjectThin can't be first
		return true
	default:
		return false
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
func (u *unmarshaller) unmarshal(value reflect.Value, typ reflect.Type) (Json, error) {
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
			return u.unmarshal(value.Elem(), value.Elem().Type())
		}
		return u.unmarshal(value.Elem(), nil)
	case reflect.Ptr:
		return u.unmarshal(value.Elem(), typ.Elem())
	case reflect.Array, reflect.Slice:
		return u.unmarshalArray(value, value.Type().Elem())
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

		return u.intern(value.String()), nil
	case reflect.Map:
		return u.unmarshalMap(value)
	case reflect.Struct:
		return u.unmarshalStruct(value)
	default:
		return nil, fmt.Errorf("json: unsupported type: %v", value.Type())
	}
}

func (u *unmarshaller) unmarshalArray(values reflect.Value, typ reflect.Type) (Json, error) {
	if values.Kind() == reflect.Slice && values.IsNil() {
		return NewNull(), nil
	}

	n := values.Len()
	a := make([]File, 0, n)

	for i := 0; i < n; i++ {
		var err error
		value, err := u.unmarshal(values.Index(i), typ)
		if err != nil {
			return nil, err
		}

		a = append(a, value)
	}

	return NewArrayCompact(a), nil
}

func (u *unmarshaller) unmarshalMap(values reflect.Value) (Json, error) {
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

			v, err := u.unmarshal(iter.Value(), elem)
			if err != nil {
				return nil, err
			}

			m[u.intern(keyStr).Value()] = v
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		for iter.Next() {
			keyStr := strconv.FormatInt(iter.Key().Int(), 10)

			v, err := u.unmarshal(iter.Value(), elem)
			if err != nil {
				return nil, err
			}

			m[u.intern(keyStr).Value()] = v
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		for iter.Next() {
			keyStr := strconv.FormatUint(iter.Key().Uint(), 10)

			v, err := u.unmarshal(iter.Value(), elem)
			if err != nil {
				return nil, err
			}

			m[u.intern(keyStr).Value()] = v
		}

	default:
		key := values.Type().Key()
		if !key.Implements(encodingTextMarshalerType) {
			return nil, fmt.Errorf("json: unsupported type %v", values.Type())
		}

		for iter.Next() {
			k := iter.Key()
			var keyStr string

			if k.Kind() != reflect.Ptr || k.IsNil() {
				// consider nil key as "" per golang 1.14, even if implements TextMarshaler.
				raw, err := k.Interface().(encoding.TextMarshaler).MarshalText()
				if err != nil {
					return nil, err
				}

				keyStr = string(raw)
			}

			v, err := u.unmarshal(iter.Value(), elem)
			if err != nil {
				return nil, err
			}

			m[u.intern(keyStr).Value()] = v
		}
	}

	return NewObjectMapCompact(m, u.keys), nil
}

func (u *unmarshaller) unmarshalStruct(values reflect.Value) (Json, error) {
	fields := internal.CachedTypeFields(values.Type())
	m := make(map[string]File, len(fields))

	for _, f := range fields {
		fv := fieldByIndex(values, f.Index)
		if !fv.IsValid() || f.OmitEmpty && isEmptyValue(fv) {
			continue
		}

		v, err := u.unmarshal(fv, fv.Type())
		if err != nil {
			return nil, err
		}

		m[u.intern(f.Name).Value()] = v
	}

	return NewObjectMapCompact(m, u.keys), nil
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
		}
		return buf, nil
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
