// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

// Package test_bootstrap implements the logic for generating Rego test mocks
// automatically from an OPA bundle and entrypoint list.
package test_bootstrap

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/loader"
	"github.com/open-policy-agent/opa/v1/logging"
)

type opts struct {
	logger         logging.Logger
	entrypoint     string
	entrypoints    []string
	annotation     string
	dataPaths      []string
	ignores        []string
	forceOverwrite bool
}

type Opt func(*opts)

func Logger(l logging.Logger) Opt {
	return func(o *opts) {
		o.logger = l
	}
}

func Entrypoint(e string) Opt {
	return func(o *opts) {
		o.entrypoint = e
	}
}

func Entrypoints(es []string) Opt {
	return func(o *opts) {
		o.entrypoints = es
	}
}

func DataPaths(ds []string) Opt {
	return func(o *opts) {
		o.dataPaths = ds
	}
}

func Ignores(i []string) Opt {
	return func(o *opts) {
		o.ignores = i
	}
}

func Force(f bool) Opt {
	return func(o *opts) {
		o.forceOverwrite = f
	}
}

func Annotation(a string) Opt {
	return func(o *opts) {
		o.annotation = a
	}
}

func StartBootstrap(_ context.Context, opt ...Opt) error {
	o := &opts{
		logger: logging.NewNoOpLogger(),
	}
	for _, opt := range opt {
		opt(o)
	}

	// Get the compiler instance with all our policies loaded up.
	compiler, err := LoadPolicies(o.dataPaths, o.ignores)
	if err != nil {
		return err
	}

	// For each rule entrypoint specified:
	// - Determine where the tests will land.
	// - Generate the testcases, based on the deps.
	out := make(map[string][]string, len(o.entrypoints))
	generatedNames := make(map[string]map[string]string, len(o.entrypoints))
	packages := make(map[string]string, len(o.entrypoints))
	for _, e := range o.entrypoints {
		rref, err := ast.ParseRef(RefPtrToQuery(e))
		if err != nil {
			return err
		}

		parentFilename := GetFileLocationForRuleRef(rref, compiler)
		testPath := path.Dir(parentFilename)
		testBaseName := path.Base(parentFilename)
		testBareFilename := strings.TrimSuffix(testBaseName, path.Ext(testBaseName))
		testFilename := path.Join(testPath, testBareFilename+"_test.rego")
		// Initialize name collision map if it doesn't already exist.
		gn := make(map[string]string)
		if value, ok := generatedNames[testFilename]; ok {
			gn = value
		}
		o.logger.Info("Generating testcases for rule '%v'. File destination will be: '%s'", rref, testFilename)

		// Iterates over all rules matching the ref and generates appropriate testcases for each.
		testcasesRaw, err := TestcasesFromRef(rref, gn, compiler)
		if err != nil {
			return err
		}
		out[testFilename] = append(out[testFilename], testcasesRaw)
		generatedNames[testFilename] = gn

		// We use lists of strings, in case multiple testcase batches need to go
		// to the same destination.
		packages[testFilename] = GetPackageForRuleRef(rref, compiler)
	}

	// Write files to disk, warning if an overwrite would occur.
	for filename, testcases := range out {
		// Skip files that already exist. Warnings are the only feedback the user will get.
		if _, err := os.Stat(filename); err == nil && !o.forceOverwrite {
			o.logger.Warn("File '%s' already exists. Skipping to avoid overwriting.", filename)
			continue
		}

		// Create the new file.
		file, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer file.Close()

		// Write package header + imports.
		if _, err := file.WriteString(packages[filename] + "_test" + "\n\nimport rego.v1"); err != nil {
			return err
		}

		// Write testcase chunks.
		for _, tc := range testcases {
			if _, err = file.WriteString("\n" + tc); err != nil {
				return err
			}
		}
	}

	return nil
}

func StartNew(_ context.Context, opt ...Opt) error {
	var entrypointRef ast.Ref

	o := &opts{
		logger: logging.NewNoOpLogger(),
	}
	for _, opt := range opt {
		opt(o)
	}

	// Get the compiler instance with all our policies loaded up.
	compiler, err := LoadPolicies(o.dataPaths, o.ignores)
	if err != nil {
		return err
	}

	if o.entrypoint != "" {
		var err error
		entrypointRef, err = ast.ParseRef(RefPtrToQuery(o.entrypoint))
		if err != nil {
			return err
		}
	}

	// Check to see if the annotation exists, and grab the rule for it.
	relevantAnnotation, err := GetRuleCustomAnnotationWithKey(o.annotation, entrypointRef, compiler)
	if err != nil {
		return err
	}
	rule := relevantAnnotation.GetRule()
	testPackage := GetPackageForRuleRef(rule.Ref(), compiler)

	// Check to see if the test file already exists, and if so, is there a conflict.
	parentFilename := GetFileLocationForRuleRef(rule.Ref(), compiler)
	testPath := path.Dir(parentFilename)
	testBaseName := path.Base(parentFilename)
	testBareFilename := strings.TrimSuffix(testBaseName, path.Ext(testBaseName))
	testFilename := path.Join(testPath, testBareFilename+"_test.rego")
	fileExists := false
	if _, err := os.Stat(testFilename); err == nil {
		fileExists = true
	}
	o.logger.Info("Generating testcases for annotation '%s'. File destination will be: '%s'", o.annotation, testFilename)

	// Compare the generated testcase names to the neighboring rules in the destination package.
	testcaseNames := []string{
		"test_success_" + o.annotation,
		"test_failure_" + o.annotation + "_no_input",
		"test_failure" + o.annotation + "_bad_input",
	}
	if fileExists {
		bs, err := os.ReadFile(testFilename)
		if err != nil {
			return err
		}

		m, err := ast.ParseModule(testFilename, string(bs))
		if err != nil {
			return err
		}

		for _, r := range m.Rules {
			for _, tc := range testcaseNames {
				if string(r.Head.Name) == tc {
					return fmt.Errorf("rule conflict for file '%s', rule '%s' already exists", testFilename, tc)
				}
			}
		}
	}

	// Actually generate the testcases, now that we *should* be in the clear.
	testcases, err := TestcasesForRule(o.annotation, rule, compiler)
	if err != nil {
		return err
	}

	// Create the new file.
	file, err := os.OpenFile(testFilename, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write package header + imports if this is the first testcase to be added to the file.
	if !fileExists {
		if _, err := file.WriteString(testPackage + "_test" + "\n\nimport rego.v1\n"); err != nil {
			return err
		}
	}

	// Write testcase chunks.
	if _, err = file.WriteString(testcases); err != nil {
		return err
	}

	return nil
}

type loaderFilter struct {
	Ignore   []string
	OnlyRego bool
}

func (f loaderFilter) Apply(abspath string, info os.FileInfo, depth int) bool {
	// if set to only load rego files, skip all non-rego files
	if f.OnlyRego && !info.IsDir() && filepath.Ext(info.Name()) != bundle.RegoExt {
		return true
	}
	for _, s := range f.Ignore {
		if loader.GlobExcludeName(s, 1)(abspath, info, depth) {
			return true
		}
	}
	return false
}

// Loads up all of the specified Rego sources. Returns an ast.Compiler instance.
func LoadPolicies(dataPaths []string, ignores []string) (*ast.Compiler, error) {
	modules := map[string]*ast.Module{}

	if len(dataPaths) > 0 {
		f := loaderFilter{
			Ignore: ignores,
		}

		result, err := loader.NewFileLoader().
			WithProcessAnnotation(true).
			Filtered(dataPaths, f.Apply)
		if err != nil {
			return nil, err
		}

		for _, m := range result.Modules {
			modules[m.Name] = m.Parsed
		}
	}

	compiler := ast.NewCompiler()
	compiler.Compile(modules)

	if compiler.Failed() {
		return nil, compiler.Errors
	}

	return compiler, nil
}

func RefPtrToQuery(rp string) string {
	var parts []string
	if !strings.HasPrefix(rp, "data.") {
		parts = append(parts, "data")
	}
	return strings.Join(append(parts, strings.Split(rp, "/")...), ".")
}

// Returns a map of targeted rule paths and their accompanying annotations refs.
func GetCustomAnnotationsForRefs(compiler *ast.Compiler) (map[string][]*ast.AnnotationsRef, error) {
	modules := compiler.Modules
	modulesList := make([]*ast.Module, 0, len(modules))
	for _, v := range modules {
		modulesList = append(modulesList, v)
	}
	as, errs := ast.BuildAnnotationSet(modulesList)
	if len(errs) > 0 {
		return nil, errs
	}
	flattened := as.Flatten()

	out := map[string][]*ast.AnnotationsRef{}
	for _, ref := range flattened {
		a := ref.Annotations
		// Filter annotations down to only rule-scoped, custom annotations that contain the key `test-bootstrap-name`.
		if a != nil && a.Scope == "rule" && len(a.Custom) > 0 {
			if _, ok := a.Custom["test-bootstrap-name"]; ok {
				targetPath := ref.Annotations.GetTargetPath().String()
				out[targetPath] = append(out[targetPath], ref)
			}
		}
	}
	return out, nil
}

// Returns a map of targeted rule paths corresponding to the `test-bootstrap-name` custom annotation.
func GetTestNamesToCustomAnnotations(compiler *ast.Compiler) (map[string][]*ast.AnnotationsRef, error) {
	modules := compiler.Modules
	modulesList := make([]*ast.Module, 0, len(modules))
	for _, v := range modules {
		modulesList = append(modulesList, v)
	}
	as, errs := ast.BuildAnnotationSet(modulesList)
	if len(errs) > 0 {
		return nil, errs
	}
	flattened := as.Flatten()

	out := map[string][]*ast.AnnotationsRef{}
	for _, ref := range flattened {
		a := ref.Annotations
		// Filter annotations down to only rule-scoped, custom annotations that contain the key `test-bootstrap-name`.
		if a != nil && a.Scope == "rule" && len(a.Custom) > 0 {
			if value, ok := a.Custom["test-bootstrap-name"]; ok {
				// Only store annotations that are valid.
				if name, ok := value.(string); ok {
					out[name] = append(out[name], ref)
				} else {
					return nil, fmt.Errorf("custom metadata key 'test-bootstrap-name' for rule at '%v' must have a string value", ref.Annotations.Location)
				}
			}
		}
	}
	return out, nil
}
