// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package test_bootstrap

// We provide 3x flavors of test cases for each top-level rule:
// - Happy path (all inputs defined correctly)
// - Missing input(s) (1+ inputs not defined)
// - Unhappy path(s) (1+ inputs incorrectly defined)
// The ideal would be to generate only the combos that matter, instead of an
// Nx(N-1) list of possible combos. For now, we get by with generating just 3x
// cases, in the hopes it'll nudge folks towards good testing practices. Later,
// we can make the test generator smarter, and infer from the policy the allowed
// datatypes of parameters.
const testRuleTemplate = `{{.TestName}} if {
	test_input = {{.Inputs}}
	{{if .Negated}}not {{end}}{{.RuleName}} with input as test_input
}`

type testRuleParams struct {
	Negated  bool
	TestName string
	RuleName string
	Inputs   string
}

// Because the text/template engine will treat nil as 'false', we're able to use
// conditional generation in these testcases.
const testTemplate = `
# Testcases generated from: {{.SourceLocation}}
{{- if .Success}}
# Success case: All inputs defined.
{{template "test" .Success}}
{{- end}}
{{- if .FailureNoInput}}
# Failure case: No inputs defined.
{{template "test" .FailureNoInput}}
{{- end}}
{{- if .FailureBadInput}}
# Failure case: Inputs defined, but wrong values.
{{template "test" .FailureBadInput}}
{{- end}}
`

type templateParams struct {
	SourceLocation                           string
	Success, FailureNoInput, FailureBadInput *testRuleParams
}
