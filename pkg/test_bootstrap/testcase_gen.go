package test_bootstrap

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/dependencies"
	"github.com/styrainc/enterprise-opa-private/pkg/internal/edittree"
)

// Note(philip): We use a custom interface type + sorting logic for improved
// test generation order later on.
type ruleSlice []*ast.Rule

func (s ruleSlice) Less(i, j int) bool {
	if s[i].Location.File == s[j].Location.File {
		return s[i].Location.Row < s[j].Location.Row
	}
	return s[i].Location.File < s[j].Location.File
}
func (s ruleSlice) Swap(i, j int) { x := s[i]; s[i] = s[j]; s[j] = x }
func (s ruleSlice) Len() int      { return len(s) }

// Note(philip): This selects the first non-nil file location found for the rule
// ref. In cases where multiple files may match the ref, this results in one of
// those files being selected *arbitrarily*. I'm not sure how to make this less
// awful, but we can at least log where things will land.
func GetFileLocationForRuleRef(ruleRef ast.Ref, compiler *ast.Compiler) string {
	rules := compiler.GetRules(ruleRef)
	locs := make([]string, 0, len(rules))

	for _, r := range rules {
		if r.Location.File != "" {
			locs = append(locs, r.Location.File)
		}
	}

	if len(locs) > 0 {
		return locs[0]
	}
	return ""
}

// Note(philip): This selects the first non-nil file package found for the rule.
func GetPackageForRuleRef(ruleRef ast.Ref, compiler *ast.Compiler) string {
	rules := compiler.GetRules(ruleRef)
	packages := make([]string, 0, len(rules))
	for _, r := range rules {
		packages = append(packages, r.Module.Package.String())
	}

	if len(packages) > 0 {
		return packages[0]
	}
	return ""
}

// This function plugs in a rule, and attempts to extract a useful input object for the testcase.
func GetInputFromRuleDeps(rule *ast.Rule, compiler *ast.Compiler) (*ast.Term, error) {
	// Grab dependencies, using opa/dependencies.
	refsBase, err := dependencies.Base(compiler, rule)
	if err != nil {
		return nil, err
	}
	refsVirtual, err := dependencies.Virtual(compiler, rule)
	if err != nil {
		return nil, err
	}

	refsAll := append(refsBase, refsVirtual...)

	// We want to generate an input object with roughly the correct structure,
	// based on the `input` refs.
	input, _, err := ASTObjectsFromRefs(refsAll, ast.StringTerm("EXAMPLE"))
	if err != nil {
		return nil, err
	}

	return input, err
}

// Note(philip): This ensures that if we encounter a long ref, like
// 'data.my.long.chain.of.refs', we can build an EditTree with the correct
// structure, such that an (*EditTree).InsertAtPath can reach the 'refs' leaf
// safely.
func VivifyTree(tree *edittree.EditTree, ref ast.Ref) {
	for i := range ref {
		if tree.Exists(ref[:i+1]) {
			continue
		}
		// Note(philip): Ideally, if the only keys at a given level are all
		// integer values, we *might* sometimes be able to infer that an array
		// is intended at that level. However, it's not a guarantee, and we have
		// to deal with folks providing whacky numerical values, like `3.145`.
		// For now, we're using Objects for everything, and we can be smarter
		// later if there's demand for it.
		tree.InsertAtPath(ref[:i+1], ast.ObjectTerm())
	}
}

// Note(philip): We want longest refs first, so that the auto-vivification of
// long tree branches will ensure we get correct structure, even if there's a
// mix of long and short refs along the same path.
type refSlice []ast.Ref

func (s refSlice) Less(i, j int) bool {
	if len(s[i]) == len(s[j]) {
		return s[i].Compare(s[j]) < 0
	}
	return len(s[i]) > len(s[j])
}
func (s refSlice) Swap(i, j int) { x := s[i]; s[i] = s[j]; s[j] = x }
func (s refSlice) Len() int      { return len(s) }

// Builds Rego AST objects, based on the refs provided to it.
// Note(philip): I wanted to do this without needing the EditTree structure from
// OPA's internals, but it certainly makes some aspects of the structure
// generation here very straightforward.
func ASTObjectsFromRefs(refs []ast.Ref, defaultLeafValue *ast.Term) (*ast.Term, *ast.Term, error) {
	tree := edittree.NewEditTree(ast.ObjectTerm(
		[2]*ast.Term{ast.VarTerm("input"), ast.ObjectTerm()},
		[2]*ast.Term{ast.VarTerm("data"), ast.ObjectTerm()},
	))
	sort.Sort(refSlice(refs))
	for _, ref := range refs {
		if tree.Exists(ref) {
			continue
		}
		VivifyTree(tree, ref)
		if len(ref) > 1 {
			if _, err := tree.InsertAtPath(ref, defaultLeafValue); err != nil {
				return nil, nil, fmt.Errorf("tree construction failed: %w", err)
			}
		}
	}

	inputItems, err := tree.RenderAtPath(ast.Ref{ast.VarTerm("input")})
	if err != nil {
		return nil, nil, fmt.Errorf("input object construction failed: %w", err)
	}

	dataItems, err := tree.RenderAtPath(ast.Ref{ast.VarTerm("data")})
	if err != nil {
		return nil, nil, fmt.Errorf("data object construction failed: %w", err)
	}

	input := ast.ObjectTerm([2]*ast.Term{ast.StringTerm("input"), inputItems})
	data := ast.ObjectTerm([2]*ast.Term{ast.StringTerm("data"), dataItems})

	return input, data, nil
}

// This function is the place where all the testcase generation pieces are
// assembled. It takes a virtual document reference, and then discovers all
// rules that apply to it. Each rule is then individually analyzed, and a
// testcase is generated for it using the testcase templates. The collected set
// of testcases is then returned as a string.
func TestcasesFromRef(ruleRef ast.Ref, compiler *ast.Compiler) (string, error) {
	// Create a new template and parse the main template.
	tmpl := template.Must(template.New("testcases").Parse(testTemplate))
	tmpl = template.Must(tmpl.New("test").Parse(testRuleTemplate))

	tests := strings.Builder{}

	rules := compiler.GetRules(ruleRef)

	// Note(philip): Sort the rules according to file and line number. This
	// helps devs map test cases to rule bodies in the original policy.
	sort.Sort(ruleSlice(rules))

	// We generate one set of testcases per rule encountered.
	for i, rule := range rules {
		tp := templateParams{}

		testName := ""
		for _, p := range ruleRef {
			if p == nil {
				return "", fmt.Errorf("nil pointer in ref: %v", ruleRef)
			}
			if s, ok := p.Value.(ast.String); ok {
				testName += "_" + strings.ReplaceAll(string(s), ".", "_")
				continue
			}
			testName += "_" + strings.ReplaceAll(p.String(), ".", "_")
		}

		testName = strings.TrimPrefix(testName, "_data")

		input, err := GetInputFromRuleDeps(rule, compiler)
		if err != nil {
			return "", err
		}

		tp.SourceLocation = rule.Location.String()
		tp.Success = &testRuleParams{
			Negated:  false,
			TestName: "test_success" + testName + "_" + strconv.FormatInt(int64(i), 10),
			RuleName: ruleRef.String(),
			Inputs:   input.String(),
		}
		tp.FailureNoInput = &testRuleParams{
			Negated:  true,
			TestName: "test_fail" + testName + "_" + strconv.FormatInt(int64(i), 10) + "_no_input",
			RuleName: ruleRef.String(),
			Inputs:   ast.ObjectTerm().String(),
		}
		tp.FailureBadInput = &testRuleParams{
			Negated:  true,
			TestName: "test_fail" + testName + "_" + strconv.FormatInt(int64(i), 10) + "_bad_input",
			RuleName: ruleRef.String(),
			Inputs:   input.String(),
		}

		tmpl.ExecuteTemplate(&tests, "testcases", tp)
		tests.WriteRune('\n')
	}

	return tests.String(), nil
}
