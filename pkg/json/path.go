package json

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const jsonPathWildcard = "*"

// Path is a parsed presentation of JSON path.
type Path []PathSeg

type InvalidPathError struct {
	error
	msg string
}

func NewInvalidPathError(msg string) InvalidPathError {
	return InvalidPathError{msg: fmt.Sprintf("json path: %s", msg)}
}

func NewInvalidPathErrorf(spec string, params ...interface{}) InvalidPathError {
	return NewInvalidPathError(fmt.Sprintf(spec, params...))
}

func (i InvalidPathError) Error() string {
	return i.msg
}

// ParsePath is a simple JSON path parser. It returns an internal
// representation of it, if no error detected while parsing.
//
// It supports limited JSON path functionality: object property
// referencing (dot notation), as well as array element referencing
// (brackets) with indices (numbers) or wildcard (*) given. In
// addition, a single use of recursion (double dot) is allowed. That
// is, it can parse the following JSON paths, for example:
//
// 1) $.foo.bar
// 2) $[0].foo.bar
// 3) $.foo[*].bar
// 4) $..* (all elements of the document with the exception of the document itself)
// 5) $..[0] (all first elements of all arrays in the document)
// 6) $..foo (all foo properties, anywhere in the document).
//
// TODO: this is not speed optimized; the construction of strings is excessive.
func ParsePath(path string) (Path, error) {
	// Strip the '$' from the beginning.

	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "$") {
		return nil, NewInvalidPathErrorf("the path has no prefix '$' ('%s')", path)
	}

	// Process the remaining the segments.

	segs := make([]PathSeg, 0)

	for p := strings.TrimSpace(path[1:]); len(p) > 0; {
		if i := strings.Index(p, "..["); i == 0 {
			segs = append(segs, NewPathRecursiveRef(true))
			p = p[2:]

		} else if i := strings.Index(p, ".."); i == 0 {
			segs = append(segs, NewPathRecursiveRef(false))
			p = p[1:]

		} else if i := strings.Index(p, "."); i == 0 {
			next := parsePathNextProperty(p[1:])
			if next == "" {
				return nil, parsePathError(path)
			}

			segs = append(segs, NewPathObjectPropertyRef(next, false))
			p = p[1+len(next):]

		} else if i := strings.Index(p, "["); i == 0 {
			trimmed := strings.TrimSpace(p[1:])
			if strings.HasPrefix(trimmed, jsonPathWildcard) {
				trimmed = strings.TrimSpace(trimmed[1:])
				if trimmed[0] != ']' {
					return nil, parsePathError(path)
				}

				segs = append(segs, NewPathArrayElementRef(-1))
				p = p[strings.Index(p, "]")+1:]

			} else if strings.HasPrefix(trimmed, "'") {
				start := strings.Index(p, "'")
				property, consumed, err := parsePathBracketProperty(p[start:])
				if err != nil {
					return nil, err
				}
				segs = append(segs, NewPathObjectPropertyRef(property, true))

				p = p[start+len(consumed):]

				if !strings.HasPrefix(strings.TrimSpace(p), "]") {
					return nil, parsePathError(path)
				}

				p = p[strings.Index(p, "]")+1:]

			} else {
				n := 0
				for i, c := range trimmed {
					if !unicode.IsNumber(c) {
						break
					}

					n = i + 1
				}

				if strings.TrimSpace(trimmed[n:])[0] != ']' {
					return nil, parsePathError(path)
				}

				index, err := strconv.ParseInt(trimmed[0:n], 10, 32)
				if err != nil {
					return nil, parsePathError(path)
				}

				// The above conversion will result in error if the index does not fit into 32-bit int.
				segs = append(segs, NewPathArrayElementRef(int(index)))
				p = p[strings.Index(p, "]")+1:]
			}
		} else {
			return nil, NewInvalidPathErrorf("illegal JSON path: %s", path)
		}
	}

	if len(segs) > 0 && segs[len(segs)-1].IsRecursive() {
		return nil, NewInvalidPathErrorf("illegal JSON path: %s", path)
	}

	r := 0
	for _, seg := range segs {
		if seg.IsRecursive() {
			r++
		}
	}

	if r > 1 {
		return nil, NewInvalidPathErrorf("illegal JSON path: %s", path)
	}

	return segs, nil
}

func parsePathNextProperty(path string) string {
	j := len(path)
	if k := strings.Index(path, "."); k >= 0 && k < j {
		j = k
	}

	if k := strings.Index(path, "["); k >= 0 && k < j {
		j = k
	}

	return path[0:j]
}

// parsePathBracketProperty consumes a quoted property name from
// the path string, returning the parsed, unquoted and unescaped name
// as well as the overall string consumed. It returns an error if
// invalid quotation was detected.
func parsePathBracketProperty(path string) (string, string, error) {
	if len(path) < 2 {
		return "", "", NewInvalidPathError("invalid quotes")
	}

	if path[0] != '\'' {
		panic("json path: invalid bracket parser invocation")
	}

	escape := false
	end := 1
	name := ""

	for i, c := range path[1:] {
		if c == '\\' {
			escape = true
			continue
		}

		if !escape && c == '\'' {
			end = i + 1
			break
		}

		escape = false
		end = i + 1
		name = name + string(c)
	}

	if path[end] != '\'' {
		return "", "", NewInvalidPathError("invalid quotes")
	}

	return name, path[0 : end+1], nil
}

func parsePathError(path string) error {
	return NewInvalidPathErrorf("illegal JSON path: %s", path)
}

func (j *Path) JoinPath(seg ...PathSeg) Path {
	newPath := make([]PathSeg, 0, len(*j)+len(seg))
	newPath = append(newPath, *j...)
	newPath = append(newPath, seg...)
	return newPath
}

func (j Path) String() string {
	var s bytes.Buffer

	s.WriteString("$")
	for _, seg := range j {
		s.WriteString(seg.String())
	}

	return s.String()
}

// Singular returns true if the path is guaranteed to point a single element.
func (j Path) Singular() bool {
	for _, seg := range j {
		if !seg.Singular() {
			return false
		}
	}

	return true
}

type PathSeg interface {
	IsArray() bool
	Index() int
	IsObject() bool
	IsObjectWildcard() bool
	IsRecursive() bool
	Property() string
	Singular() bool
	fmt.Stringer
}

type PathArrayElementRef struct {
	index int
}

type PathObjectPropertyRef struct {
	property string
	brackets bool
}

type PathRecursiveRef struct {
	double bool
}

func NewPathArrayElementRef(index int) *PathArrayElementRef {
	return &PathArrayElementRef{index: index}
}

func (j *PathArrayElementRef) IsArray() bool {
	return true
}

func (j *PathArrayElementRef) Index() int {
	return j.index
}

func (j *PathArrayElementRef) IsObject() bool {
	return false
}

func (j *PathArrayElementRef) IsObjectWildcard() bool {
	return false
}

func (j *PathArrayElementRef) IsRecursive() bool {
	return false
}

func (j *PathArrayElementRef) Property() string {
	return ""
}

func (j *PathArrayElementRef) String() string {
	if j.index >= 0 {
		return fmt.Sprintf("[%d]", j.index)
	}
	return "[*]"
}

func (j *PathArrayElementRef) Singular() bool {
	return j.index >= 0
}

func NewPathObjectPropertyRef(property string, brackets bool) *PathObjectPropertyRef {
	return &PathObjectPropertyRef{property: property, brackets: brackets}
}

func (j *PathObjectPropertyRef) IsArray() bool {
	return false
}

func (j *PathObjectPropertyRef) Index() int {
	return -1
}

func (j *PathObjectPropertyRef) IsObject() bool {
	return true
}

func (j *PathObjectPropertyRef) IsObjectWildcard() bool {
	return j.property == jsonPathWildcard
}

func (j *PathObjectPropertyRef) IsRecursive() bool {
	return false
}

func (j *PathObjectPropertyRef) Property() string {
	return j.property
}

func (j *PathObjectPropertyRef) String() string {
	if j.brackets {
		return fmt.Sprintf("['%s']", strings.Replace(j.property, "'", "\\'", -1))
	}
	return fmt.Sprintf(".%s", j.property)
}

func (j *PathObjectPropertyRef) Singular() bool {
	return !j.IsObjectWildcard()
}

func NewPathRecursiveRef(double bool) *PathRecursiveRef {
	return &PathRecursiveRef{double}
}

func (j *PathRecursiveRef) IsArray() bool {
	return false
}

func (j *PathRecursiveRef) Index() int {
	return -1
}

func (j *PathRecursiveRef) IsObject() bool {
	return false
}

func (j *PathRecursiveRef) IsObjectWildcard() bool {
	return false
}

func (j *PathRecursiveRef) IsRecursive() bool {
	return true
}

func (j *PathRecursiveRef) Property() string {
	return ""
}

func (j *PathRecursiveRef) String() string {
	if j.double {
		return ".."
	}
	return "."
}

func (j *PathRecursiveRef) Singular() bool {
	return false
}

func PathsToStrings(paths []Path) []string {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		result = append(result, p.String())
	}
	return result
}
