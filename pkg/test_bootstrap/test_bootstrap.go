// Package test_bootstrap implements the logic for generating Rego test mocks
// automatically from an OPA bundle and entrypoint list.
package test_bootstrap

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/loader"
	"github.com/open-policy-agent/opa/logging"
)

type opts struct {
	logger         logging.Logger
	entrypoints    []string
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

func Start(ctx context.Context, opt ...Opt) error {
	_, cancel := context.WithCancel(ctx)
	defer cancel()

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
		o.logger.Info("Generating testcases for rule '%v'. File destination will be: %v", rref, testFilename)

		// Iterates over all rules matching the ref and generates appropriate testcases for each.
		testcasesRaw, err := TestcasesFromRef(rref, compiler)
		if err != nil {
			return err
		}
		out[testFilename] = append(out[testFilename], testcasesRaw)

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
