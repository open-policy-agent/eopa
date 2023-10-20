package library

import (
	"io"
	"io/fs"
	"path/filepath"

	embedded "github.com/styrainc/enterprise-opa-private/library"

	"github.com/open-policy-agent/opa/ast"
)

var modules map[string]*ast.Module
var packages map[string]string // "data.system.eopa.utils.dynamodb.v1" -> its filename == key in modules

func Init() (err error) {
	ast.DefaultModuleLoader(loader)
	modules, packages, err = toMap(embedded.Library)
	return
}

func loader(res map[string]*ast.Module) (map[string]*ast.Module, error) {
	extras := make(map[string]*ast.Module)
	for _, mod := range res {
		ast.WalkRefs(mod, func(a ast.Ref) bool {
			f, found := packages[a[:len(a)-1].String()]
			if found {
				if _, done := extras[f]; done {
					return false // skip this one, dealt with already
				}
				if _, loaded := res[f]; !loaded { // avoid loading what was loaded already again
					extras[f] = modules[f]
				}
			}
			return found
		})
	}
	return extras, nil
}

func toMap(fsys fs.FS) (map[string]*ast.Module, map[string]string, error) {
	mods := make(map[string]*ast.Module)
	pkgs := make(map[string]string)
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
			pkgs[mods[p].Package.Path.String()] = p
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return mods, pkgs, nil
}
