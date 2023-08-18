package library

import (
	"io"
	"io/fs"
	"path/filepath"

	embedded "github.com/styrainc/enterprise-opa-private/library"

	"github.com/open-policy-agent/opa/ast"
)

var modules map[string]*ast.Module

func Init() (err error) {
	ast.DefaultModuleLoader(loader)
	modules, err = toMap(embedded.Library)
	return
}

// TODO(sr): Right now, we'll unconditionally inject our library modules.
// That'll cause extra work for the compiler which may be unnecessary in
// most cases. The optimization here would be to check the argument to
// `loader` and see if any of the provided functions is imported at all.
func loader(res map[string]*ast.Module) (map[string]*ast.Module, error) {
	_, done := res[random(modules)]
	if !done {
		return modules, nil
	}
	return nil, nil
}

func random(m map[string]*ast.Module) string {
	for k := range m {
		return k
	}
	panic("unreachable")
}

func toMap(fsys fs.FS) (map[string]*ast.Module, error) {
	mods := make(map[string]*ast.Module)
	if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if filepath.Ext(p) == ".rego" {
			fd, err := fsys.Open(p)
			if err != nil {
				return err
			}
			bs, err := io.ReadAll(fd)
			if err != nil {
				return err
			}
			mods[p], err = ast.ParseModule(p, string(bs))
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return mods, nil
}
