package compile

import (
	"fmt"
	"maps"
	"strings"
)

type Builtins map[string]struct{}

func NewBuiltins(strs ...string) Builtins {
	return make(Builtins).Add(strs...)
}

func (s Builtins) Clone() Builtins {
	return maps.Clone(s)
}

func (s Builtins) Add(strs ...string) Builtins {
	for i := range strs {
		s[strs[i]] = struct{}{}
	}
	return s
}

// Contains checks if a string exists in the set
func (s Builtins) Contains(str string) bool {
	_, exists := s[str]
	return exists
}

// Intersection returns a new set containing elements present in both sets
func (s Builtins) Intersection(other Builtins) Builtins {
	result := NewBuiltins()
	// Iterate through the smaller set for better performance
	if len(s) > len(other) {
		s, other = other, s
	}
	for elem := range s {
		if other.Contains(elem) {
			result.Add(elem)
		}
	}
	return result
}

// Constraint lets us limit the builtins that are allowed in a translation.
// There are hardcoded sets of supported builtins. The constraints become
// effective during the post-PE analysis (compile.Checks()).
type Constraint struct {
	Target   string
	Variant  string
	Builtins Builtins
}

// NewConstraints returns a new Constraint object based on the type
// requested, ucast or sql.
func NewConstraints(typ, variant string) *Constraint {
	c := Constraint{Target: strings.ToUpper(typ), Variant: variant}
	switch typ {
	case "sql":
		c.Builtins = sqlBuiltins
	case "ucast":
		switch strings.ToLower(variant) {
		case "all":
			c.Builtins = allBuiltins
		case "linq":
			c.Variant = "LINQ" // normalize spelling
			c.Builtins = ucastLINQBuiltins
		default:
			c.Variant = ""
			c.Builtins = ucastBuiltins
		}
	default:
		c.Builtins = allBuiltins
	}

	return &c
}

// Supports allows us to encode more fluent constraints, like support for "not"
func (c *Constraint) Supports(x string) bool {
	switch x {
	case "not":
		// only SQL and ucast/all support general `NOT (...)` negation
		return c.Target != "UCAST" || c.Variant == "all"
	}

	return false
}

func (c *Constraint) String() string {
	if c.Variant == "" {
		return c.Target
	}
	return fmt.Sprintf("%s (%s)", c.Target, c.Variant)
}

var (
	// So far, we don't need to differentiate between the SQL dialects,
	// they all can do the builtins we currently translate.
	sqlBuiltins = allBuiltins

	ucastBuiltins = NewBuiltins(
		"eq",
		"neq",
		"lt",
		"lte",
		"gt",
		"gte",
	)

	allBuiltins = ucastBuiltins.Clone().Add(
		"internal.member_2",
		// "nin", // TODO: deal with NOT IN
		"startswith",
		"endswith",
		"contains",
	)
	ucastLINQBuiltins = ucastBuiltins.Clone().Add(
		"internal.member_2",
		//"nin", // TODO: deal with NOT IN
	)
)
