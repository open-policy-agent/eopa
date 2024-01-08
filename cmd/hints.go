package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/cobra"
	"golang.org/x/exp/maps"
)

const styra = `https://docs.styra.com/opa/errors/`

var hints = map[string]*regexp.Regexp{
	`eval-conflict-error/complete-rules-must-not-produce-multiple-outputs`: regexp.MustCompile(`^eval_conflict_error: complete rules must not produce multiple outputs$`),
	`eval-conflict-error/object-keys-must-be-unique`:                       regexp.MustCompile(`^object insert conflict$|^eval_conflict_error: object keys must be unique$`),
	`rego-unsafe-var-error/var-name-is-unsafe`:                             regexp.MustCompile(`^rego_unsafe_var_error: var .* is unsafe$`),
	`rego-recursion-error/rule-name-is-recursive`:                          regexp.MustCompile(`^rego_recursion_error: rule .* is recursive:`),
	`rego-parse-error/var-cannot-be-used-for-rule-name`:                    regexp.MustCompile(`^rego_parse_error: var cannot be used for rule name$`),
	`rego-type-error/conflicting-rules-name-found`:                         regexp.MustCompile(`^rego_type_error: conflicting rules .* found$`),
	`rego-type-error/match-error`:                                          regexp.MustCompile(`^rego_type_error: match error`),
	`rego-type-error/arity-mismatch`:                                       regexp.MustCompile(`^rego_type_error: .*: arity mismatch`),
	`rego-type-error/function-has-arity-got-argument`:                      regexp.MustCompile(`^rego_type_error: function .* has arity [0-9]+, got [0-9]+ arguments?$`),
	`rego-compile-error/assigned-var-name-unused`:                          regexp.MustCompile(`^rego_compile_error: assigned var .* unused$`),
	`rego-parse-error/unexpected-assign-token`:                             regexp.MustCompile(`^rego_parse_error: unexpected assign token:`),
	`rego-parse-error/unexpected-identifier-token`:                         regexp.MustCompile(`^rego_parse_error: unexpected identifier token:`),
	`rego-parse-error/unexpected-left-curly-token`:                         regexp.MustCompile(`^rego_parse_error: unexpected { token:`),
	`rego-parse-error/unexpected-right-curly-token`:                        regexp.MustCompile(`^rego_parse_error: unexpected } token`),
	`rego-parse-error/unexpected-name-keyword`:                             regexp.MustCompile(`^rego_parse_error: unexpected .* keyword:`),
	`rego-parse-error/unexpected-string-token`:                             regexp.MustCompile(`^rego_parse_error: unexpected string token:`),
	`rego-type-error/multiple-default-rules-name-found`:                    regexp.MustCompile(`^rego_type_error: multiple default rules .* found$`),
}

func extraHints(c *cobra.Command, e error) error {
	f := c.Flag("format")
	enable := f != nil && f.Value.String() == "pretty"
	if !enable || e == nil {
		return e
	}
	e0 := unwrapToFP(e)
	msgs, _ := extractMessages(e0)
	us := map[string]struct{}{}

	for u, r := range hints {
		for _, msg := range msgs {
			if r.MatchString(msg) {
				us[u] = struct{}{}
			}
		}
	}
	if len(us) == 0 {
		return e
	}

	hs := maps.Keys(us)
	sort.Strings(hs)
	hints := strings.Builder{}
	hints.WriteString("For more information, see:")
	for i := range hs {
		if len(us) > 1 {
			hints.WriteRune('\n')
			hints.WriteString("- ")
		} else {
			hints.WriteRune(' ')
		}
		hints.WriteString(styra)
		hints.WriteString(hs[i])
		hints.WriteRune('\n')
	}
	fmt.Fprint(os.Stderr, hints.String())
	return e
}

func unwrapToFP(e error) error {
	if w := errors.Unwrap(e); w != nil {
		return unwrapToFP(w)
	}
	return e
}

// NOTE(sr): This setup allows us to extract the output error messages
// although its types are internal (to OPA). We'll match based on the
// json struct tags, just like encoding/json.
type message struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func extractMessages(e error) ([]string, error) {
	msgs := []message{}
	decoder, err := mapstructure.NewDecoder(
		&mapstructure.DecoderConfig{
			TagName: "json",
			Result:  &msgs,
		},
	)
	if err != nil {
		return nil, err
	}
	if err := decoder.Decode(e); err != nil {
		return nil, err
	}
	m := make([]string, len(msgs))
	for i := range msgs {
		m[i] = msgs[i].Code
		if m[i] != "" {
			m[i] += ": "
		}
		m[i] += msgs[i].Message
	}
	return m, nil
}
