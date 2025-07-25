// Copyright 2019 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package bundle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"strings"

	bjson "github.com/open-policy-agent/eopa/pkg/json"

	"github.com/open-policy-agent/opa/v1/ast"
	bundleApi "github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/util"
	"github.com/open-policy-agent/eopa/pkg/internal/json/patch"
)

// BundlesBasePath is the storage path used for storing bundle metadata
var BundlesBasePath = storage.MustParsePath("/system/bundles")

var ModulesInfoBasePath = storage.MustParsePath("/system/modules")

// Note: As needed these helpers could be memoized.

// ManifestStoragePath is the storage path used for the given named bundle manifest.
func ManifestStoragePath(name string) storage.Path {
	return append(BundlesBasePath, name, "manifest")
}

// EtagStoragePath is the storage path used for the given named bundle etag.
func EtagStoragePath(name string) storage.Path {
	return append(BundlesBasePath, name, "etag")
}

func namedBundlePath(name string) storage.Path {
	return append(BundlesBasePath, name)
}

func rootsPath(name string) storage.Path {
	return append(BundlesBasePath, name, "manifest", "roots")
}

func revisionPath(name string) storage.Path {
	return append(BundlesBasePath, name, "manifest", "revision")
}

func wasmModulePath(name string) storage.Path {
	return append(BundlesBasePath, name, "wasm")
}

func wasmEntrypointsPath(name string) storage.Path {
	return append(BundlesBasePath, name, "manifest", "wasm")
}

func metadataPath(name string) storage.Path {
	return append(BundlesBasePath, name, "manifest", "metadata")
}

func moduleRegoVersionPath(id string) storage.Path {
	return append(ModulesInfoBasePath, strings.Trim(id, "/"), "rego_version")
}

func moduleInfoPath(id string) storage.Path {
	return append(ModulesInfoBasePath, strings.Trim(id, "/"))
}

// ReadBundleNamesFromStore will return a list of bundle names which have had their metadata stored.
func ReadBundleNamesFromStore(ctx context.Context, store storage.Store, txn storage.Transaction) ([]string, error) {
	value, err := store.Read(ctx, txn, BundlesBasePath)
	if err != nil {
		return nil, err
	}

	if value == nil {
		return []string{}, nil
	}

	// TODO: Udpate to be of type bjson.Object
	bundleMap, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("corrupt manifest roots")
	}

	bundles := make([]string, len(bundleMap))
	idx := 0
	for name := range bundleMap {
		bundles[idx] = name
		idx++
	}
	return bundles, nil
}

// WriteManifestToStore will write the manifest into the storage. This function is called when
// the bundle is activated.
func WriteManifestToStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string, manifest bundleApi.Manifest) error {
	return write(ctx, store, txn, ManifestStoragePath(name), bjson.MustNew(manifest))
}

// WriteEtagToStore will write the bundle etag into the storage. This function is called when the bundle is activated.
func WriteEtagToStore(ctx context.Context, store storage.Store, txn storage.Transaction, name, etag string) error {
	return write(ctx, store, txn, EtagStoragePath(name), bjson.MustNew(etag))
}

func write(ctx context.Context, store storage.Store, txn storage.Transaction, path storage.Path, value bjson.Json) error {
	value = value.Clone(true).(bjson.Json)

	var dir []string
	if len(path) > 1 {
		dir = path[:len(path)-1]
	}

	if err := storage.MakeDir(ctx, store, txn, dir); err != nil {
		return err
	}

	return store.Write(ctx, txn, storage.AddOp, path, value)
}

// EraseManifestFromStore will remove the manifest from storage. This function is called
// when the bundle is deactivated.
func EraseManifestFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) error {
	path := namedBundlePath(name)
	err := store.Write(ctx, txn, storage.RemoveOp, path, bjson.MustNew(nil))
	return suppressNotFound(err)
}

// eraseBundleEtagFromStore will remove the bundle etag from storage. This function is called
// when the bundle is deactivated.
func eraseBundleEtagFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) error {
	path := EtagStoragePath(name)
	err := store.Write(ctx, txn, storage.RemoveOp, path, bjson.MustNew(nil))
	return suppressNotFound(err)
}

func suppressNotFound(err error) error {
	if err == nil || storage.IsNotFound(err) {
		return nil
	}
	return err
}

func writeWasmModulesToStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string, b *bundleApi.Bundle) error {
	basePath := wasmModulePath(name)
	for _, wm := range b.WasmModules {
		path := append(basePath, wm.Path)
		err := write(ctx, store, txn, path, bjson.MustNew(base64.StdEncoding.EncodeToString(wm.Raw)))
		if err != nil {
			return err
		}
	}
	return nil
}

func eraseWasmModulesFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) error {
	path := wasmModulePath(name)

	err := store.Write(ctx, txn, storage.RemoveOp, path, bjson.MustNew(nil))
	return suppressNotFound(err)
}

func eraseModuleRegoVersionsFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, modules []string) error {
	for _, module := range modules {
		err := store.Write(ctx, txn, storage.RemoveOp, moduleInfoPath(module), nil)
		if err := suppressNotFound(err); err != nil {
			return err
		}
	}
	return nil
}

// ReadWasmMetadataFromStore will read Wasm module resolver metadata from the store.
func ReadWasmMetadataFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) ([]bundleApi.WasmResolver, error) {
	path := wasmEntrypointsPath(name)
	value, err := store.Read(ctx, txn, path)
	if err != nil {
		return nil, err
	}

	bs, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("corrupt wasm manifest data")
	}

	var wasmMetadata []bundleApi.WasmResolver

	err = util.UnmarshalJSON(bs, &wasmMetadata)
	if err != nil {
		return nil, fmt.Errorf("corrupt wasm manifest data")
	}

	return wasmMetadata, nil
}

// ReadWasmModulesFromStore will write Wasm module resolver metadata from the store.
func ReadWasmModulesFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) (map[string][]byte, error) {
	path := wasmModulePath(name)
	value, err := store.Read(ctx, txn, path)
	if err != nil {
		return nil, err
	}

	encodedModules, ok := value.(bjson.Object)
	if !ok {
		return nil, fmt.Errorf("corrupt wasm modules")
	}

	rawModules := map[string][]byte{}
	for _, path := range encodedModules.Names() {
		enc := encodedModules.Value(path)
		encStr, ok := enc.(*bjson.String)
		if !ok {
			return nil, fmt.Errorf("corrupt wasm modules")
		}
		bs, err := base64.StdEncoding.DecodeString(encStr.Value())
		if err != nil {
			return nil, err
		}
		rawModules[path] = bs
	}
	return rawModules, nil
}

// ReadBundleRootsFromStore returns the roots in the specified bundle.
// If the bundle is not activated, this function will return
// storage NotFound error.
func ReadBundleRootsFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) ([]string, error) {
	value, err := store.Read(ctx, txn, rootsPath(name))
	if err != nil {
		return nil, err
	}

	if value == nil {
		return []string{}, nil
	}

	// TODO: Udpate manifest roots to be of type bjson.Array
	sl, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("corrupt manifest roots")
	}

	roots := make([]string, len(sl))

	for i := range sl {
		roots[i], ok = sl[i].(string)
		if !ok {
			return nil, fmt.Errorf("corrupt manifest root")
		}
	}

	return roots, nil
}

// ReadBundleRevisionFromStore returns the revision in the specified bundle.
// If the bundle is not activated, this function will return
// storage NotFound error.
func ReadBundleRevisionFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) (string, error) {
	return readRevisionFromStore(ctx, store, txn, revisionPath(name))
}

func readRevisionFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, path storage.Path) (string, error) {
	value, err := store.Read(ctx, txn, path)
	if err != nil {
		return "", err
	}

	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("corrupt manifest revision")
	}

	return str, nil
}

// ReadBundleMetadataFromStore returns the metadata in the specified bundle.
// If the bundle is not activated, this function will return
// storage NotFound error.
func ReadBundleMetadataFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) (map[string]interface{}, error) {
	return readMetadataFromStore(ctx, store, txn, metadataPath(name))
}

func readMetadataFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, path storage.Path) (map[string]interface{}, error) {
	value, err := store.Read(ctx, txn, path)
	if err != nil {
		return nil, suppressNotFound(err)
	}

	data, ok := value.(bjson.Object)
	if !ok {
		return nil, fmt.Errorf("corrupt manifest metadata")
	}

	return data.JSON().(map[string]interface{}), nil
}

// ReadBundleEtagFromStore returns the etag for the specified bundle.
// If the bundle is not activated, this function will return
// storage NotFound error.
func ReadBundleEtagFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, name string) (string, error) {
	return readEtagFromStore(ctx, store, txn, EtagStoragePath(name))
}

func readEtagFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, path storage.Path) (string, error) {
	value, err := store.Read(ctx, txn, path)
	if err != nil {
		return "", err
	}

	str, ok := value.(*bjson.String)
	if !ok {
		return "", fmt.Errorf("corrupt bundle etag")
	}

	return str.Value(), nil
}

type CustomActivator struct{}

// Activate the bundle(s) by loading into the given Store. This will load policies, data, and record
// the manifest in storage. The compiler provided will have had the polices compiled on it.
func (*CustomActivator) Activate(opts *bundleApi.ActivateOpts) error {
	return activateBundles(opts)
}

// Note(philip): Originally, this function would convert the bundle in-place to
// answer some validation queries. The converted objects would be thrown away,
// meaning the (*inmem.store).Truncate() call later would have to redo all the
// conversion work again. For larger (>1 GB) OPA bundles, this resulted in
// prohibitive slowdowns.
func activateBundles(opts *bundleApi.ActivateOpts) error {
	// Build collections of bundle names, modules, and roots to erase
	erase := map[string]struct{}{}
	names := map[string]struct{}{}
	deltaBundles := map[string]*bundleApi.Bundle{}
	snapshotBundles := map[string]*bundleApi.Bundle{}

	for name, b := range opts.Bundles {
		if b.Type() == bundleApi.DeltaBundleType {
			deltaBundles[name] = b
		} else {
			snapshotBundles[name] = b
			names[name] = struct{}{}

			roots, err := ReadBundleRootsFromStore(opts.Ctx, opts.Store, opts.Txn, name)
			if suppressNotFound(err) != nil {
				return err
			}
			for _, root := range roots {
				erase[root] = struct{}{}
			}

			// Erase data at new roots to prepare for writing the new data
			for _, root := range *b.Manifest.Roots {
				erase[root] = struct{}{}
			}
		}
	}

	// Before changing anything make sure the roots don't collide with any
	// other bundles that already are activated or other bundles being activated.
	err := hasRootsOverlap(opts.Ctx, opts.Store, opts.Txn, opts.Bundles)
	if err != nil {
		return err
	}

	if len(deltaBundles) != 0 {
		err := activateDeltaBundles(opts, deltaBundles)
		if err != nil {
			return err
		}
	}

	// Erase data and policies at new + old roots, and remove the old
	// manifests before activating a new snapshot bundle.
	remaining, err := eraseBundles(opts.Ctx, opts.Store, opts.Txn, opts.ParserOptions, names, erase)
	if err != nil {
		return err
	}

	// Note(philip): This block does in in-place replacement of the JSON
	// bundleApi.Raw content for OPA bundles with their EOPA BJSON equivalents.
	// This saves re-processing the data multiple times down the line,
	// noticeably in (*inmem.store).Truncate.
	for _, b := range snapshotBundles {
		for idx, item := range b.Raw {
			path := filepath.ToSlash(item.Path)
			if filepath.Base(path) == "data.json" {
				val, err := BjsonFromBinary(item.Value)
				if err != nil {
					return err
				}

				bs, err := bjson.Marshal(val)
				if err != nil {
					return err
				}
				b.Raw[idx] = bundleApi.Raw{Path: item.Path, Value: bs}
			}
		}
	}

	// Validate data in bundle does not contain paths outside the bundle's roots.
	for _, b := range snapshotBundles {
		for _, item := range b.Raw {
			path := filepath.ToSlash(item.Path)

			if filepath.Base(path) == "data.json" {
				val, err := BjsonFromBinary(item.Value)
				if err != nil {
					return err
				}

				valObj, ok := val.(bjson.Object)
				if ok {
					err := doDFS(valObj, filepath.Dir(strings.Trim(path, "/")), *b.Manifest.Roots)
					if err != nil {
						return err
					}
				} else {
					// Build an object for the value
					p := getNormalizedPath(path)

					if len(p) == 0 {
						return fmt.Errorf("root value must be object")
					}

					dir := bjson.NewObject(nil)
					for i := len(p) - 1; i > 0; i-- {
						dir, _ = dir.Set(p[i], val)
						val = dir
						dir = bjson.NewObject(nil)
					}
					dir, _ = dir.Set(p[0], val)

					err = doDFS(dir, filepath.Dir(strings.Trim(path, "/")), *b.Manifest.Roots)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	// Compile the modules all at once to avoid having to re-do work.
	remainingAndExtra := make(map[string]*ast.Module, len(remaining)+len(opts.ExtraModules))
	maps.Copy(remainingAndExtra, remaining)
	maps.Copy(remainingAndExtra, opts.ExtraModules)

	err = compileModules(opts.Compiler, opts.Metrics, snapshotBundles, remainingAndExtra, false)
	if err != nil {
		return err
	}

	if err := writeDataAndModules(opts.Ctx, opts.Store, opts.Txn, opts.TxnCtx, snapshotBundles, false, opts.ParserOptions.RegoVersion); err != nil {
		return err
	}

	if err := ast.CheckPathConflicts(opts.Compiler, storage.NonEmpty(opts.Ctx, opts.Store, opts.Txn)); len(err) > 0 {
		return err
	}

	for name, b := range snapshotBundles {
		if err := writeManifestToStore(opts, name, b.Manifest); err != nil {
			return err
		}

		if err := writeEtagToStore(opts, name, b.Etag); err != nil {
			return err
		}

		if err := writeWasmModulesToStore(opts.Ctx, opts.Store, opts.Txn, name, b); err != nil {
			return err
		}
	}

	return nil
}

func doDFS(obj bjson.Object, path string, roots []string) error {
	if len(roots) == 1 && roots[0] == "" {
		return nil
	}

	for _, key := range obj.Names() {

		newPath := filepath.Join(strings.Trim(path, "/"), key)

		// Note: filepath.Join can return paths with '\' separators, always use
		// filepath.ToSlash to keep them normalized.
		newPath = strings.TrimLeft(filepath.ToSlash(newPath), "/.")

		contains := false
		prefix := false
		if bundleApi.RootPathsContain(roots, newPath) {
			contains = true
		} else {
			for i := range roots {
				if strings.HasPrefix(strings.Trim(roots[i], "/"), newPath) {
					prefix = true
					break
				}
			}
		}

		if !contains && !prefix {
			return fmt.Errorf("manifest roots %v do not permit data at path '/%s' (hint: check bundle directory structure)", roots, newPath)
		}

		if contains {
			continue
		}

		next, ok := obj.Value(key).(bjson.Object)
		if !ok {
			return fmt.Errorf("manifest roots %v do not permit data at path '/%s' (hint: check bundle directory structure)", roots, newPath)
		}

		if err := doDFS(next, newPath, roots); err != nil {
			return err
		}
	}
	return nil
}

func activateDeltaBundles(opts *bundleApi.ActivateOpts, bundles map[string]*bundleApi.Bundle) error {
	// Check that the manifest roots and wasm resolvers in the delta bundle
	// match with those currently in the store
	for name, b := range bundles {
		value, err := opts.Store.Read(opts.Ctx, opts.Txn, ManifestStoragePath(name))
		if err != nil {
			if storage.IsNotFound(err) {
				continue
			}
			return err
		}

		var val interface{}
		switch v := value.(type) {
		case bjson.Json:
			val = v.JSON()
		default:
			val = v
		}
		bs, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("corrupt manifest data: %w", err)
		}

		var manifest bundleApi.Manifest

		err = util.UnmarshalJSON(bs, &manifest)
		if err != nil {
			return fmt.Errorf("corrupt manifest data: %w", err)
		}

		if !equalWasmResolversAndRoots(b.Manifest, manifest) {
			return fmt.Errorf("delta bundle '%s' has wasm resolvers or manifest roots that are different from those in the store", name)
		}
	}

	for _, b := range bundles {
		err := applyPatches(opts.Ctx, opts.Store, opts.Txn, b.Patch.Data)
		if err != nil {
			return err
		}
	}

	if err := ast.CheckPathConflicts(opts.Compiler, storage.NonEmpty(opts.Ctx, opts.Store, opts.Txn)); len(err) > 0 {
		return err
	}

	for name, b := range bundles {
		if err := writeManifestToStore(opts, name, b.Manifest); err != nil {
			return err
		}

		if err := writeEtagToStore(opts, name, b.Etag); err != nil {
			return err
		}
	}

	return nil
}

// erase bundles by name and roots. This will clear all policies and data at its roots and remove its
// manifest from storage.
func eraseBundles(ctx context.Context, store storage.Store, txn storage.Transaction, parserOpts ast.ParserOptions, names map[string]struct{}, roots map[string]struct{}) (map[string]*ast.Module, error) {
	if err := eraseData(ctx, store, txn, roots); err != nil {
		return nil, err
	}

	remaining, removed, err := erasePolicies(ctx, store, txn, parserOpts, roots)
	if err != nil {
		return nil, err
	}

	for name := range names {
		if err := EraseManifestFromStore(ctx, store, txn, name); suppressNotFound(err) != nil {
			return nil, err
		}

		if err := LegacyEraseManifestFromStore(ctx, store, txn); suppressNotFound(err) != nil {
			return nil, err
		}

		if err := eraseBundleEtagFromStore(ctx, store, txn, name); suppressNotFound(err) != nil {
			return nil, err
		}

		if err := eraseWasmModulesFromStore(ctx, store, txn, name); suppressNotFound(err) != nil {
			return nil, err
		}
	}

	err = eraseModuleRegoVersionsFromStore(ctx, store, txn, removed)
	if err != nil {
		return nil, err
	}

	return remaining, nil
}

func eraseData(ctx context.Context, store storage.Store, txn storage.Transaction, roots map[string]struct{}) error {
	for root := range roots {
		path, ok := storage.ParsePathEscaped("/" + root)
		if !ok {
			return fmt.Errorf("manifest root path invalid: %v", root)
		}

		if len(path) > 0 {
			if err := store.Write(ctx, txn, storage.RemoveOp, path, bjson.MustNew(nil)); suppressNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}

type moduleInfo struct {
	RegoVersion ast.RegoVersion `json:"rego_version"`
}

func readModuleInfoFromStore(ctx context.Context, store storage.Store, txn storage.Transaction) (map[string]moduleInfo, error) {
	value, err := store.Read(ctx, txn, ModulesInfoBasePath)
	if suppressNotFound(err) != nil {
		return nil, err
	}

	if value == nil {
		return nil, nil
	}

	if m, ok := value.(map[string]any); ok {
		versions := make(map[string]moduleInfo, len(m))

		for k, v := range m {
			if m0, ok := v.(map[string]any); ok {
				if ver, ok := m0["rego_version"]; ok {
					if vs, ok := ver.(json.Number); ok {
						i, err := vs.Int64()
						if err != nil {
							return nil, fmt.Errorf("corrupt rego version")
						}
						versions[k] = moduleInfo{RegoVersion: ast.RegoVersionFromInt(int(i))}
					}
				}
			}
		}
		return versions, nil
	}

	return nil, fmt.Errorf("corrupt rego version")
}

func erasePolicies(ctx context.Context, store storage.Store, txn storage.Transaction, parserOpts ast.ParserOptions, roots map[string]struct{}) (map[string]*ast.Module, []string, error) {
	ids, err := store.ListPolicies(ctx, txn)
	if err != nil {
		return nil, nil, err
	}

	modulesInfo, err := readModuleInfoFromStore(ctx, store, txn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read module info from store: %w", err)
	}

	getRegoVersion := func(modId string) (ast.RegoVersion, bool) {
		info, ok := modulesInfo[modId]
		if !ok {
			return ast.RegoUndefined, false
		}
		return info.RegoVersion, true
	}

	remaining := map[string]*ast.Module{}
	var removed []string

	for _, id := range ids {
		bs, err := store.GetPolicy(ctx, txn, id)
		if err != nil {
			return nil, nil, err
		}

		parserOptsCpy := parserOpts
		if regoVersion, ok := getRegoVersion(id); ok {
			parserOptsCpy.RegoVersion = regoVersion
		}

		module, err := ast.ParseModuleWithOpts(id, string(bs), parserOptsCpy)
		if err != nil {
			return nil, nil, err
		}
		path, err := module.Package.Path.Ptr()
		if err != nil {
			return nil, nil, err
		}
		deleted := false
		for root := range roots {
			if bundleApi.RootPathsContain([]string{root}, path) {
				if err := store.DeletePolicy(ctx, txn, id); err != nil {
					return nil, nil, err
				}
				deleted = true
				break
			}
		}

		if deleted {
			removed = append(removed, id)
		} else {
			remaining[id] = module
		}
	}

	return remaining, removed, nil
}

func writeManifestToStore(opts *bundleApi.ActivateOpts, name string, manifest bundleApi.Manifest) error {
	// Always write manifests to the named location. If the plugin is in the older style config
	// then also write to the old legacy unnamed location.
	return WriteManifestToStore(opts.Ctx, opts.Store, opts.Txn, name, manifest)
}

func writeEtagToStore(opts *bundleApi.ActivateOpts, name, etag string) error {
	return WriteEtagToStore(opts.Ctx, opts.Store, opts.Txn, name, etag)
}

func writeModuleRegoVersionToStore(ctx context.Context, store storage.Store, txn storage.Transaction, b *bundleApi.Bundle,
	mf bundleApi.ModuleFile, storagePath string, runtimeRegoVersion ast.RegoVersion,
) error {
	var regoVersion ast.RegoVersion
	if mf.Parsed != nil {
		regoVersion = mf.Parsed.RegoVersion()
	}

	if regoVersion == ast.RegoUndefined {
		var err error
		regoVersion, err = b.RegoVersionForFile(mf.Path, ast.RegoUndefined)
		if err != nil {
			return fmt.Errorf("failed to get rego version for module '%s' in bundle: %w", mf.Path, err)
		}
	}

	if regoVersion != ast.RegoUndefined && regoVersion != runtimeRegoVersion {
		if err := write(ctx, store, txn, moduleRegoVersionPath(storagePath), bjson.NewFloatInt(int64(regoVersion.Int()))); err != nil {
			return fmt.Errorf("failed to write rego version for module '%s': %w", storagePath, err)
		}
	}
	return nil
}

func writeDataAndModules(ctx context.Context, store storage.Store, txn storage.Transaction, txnCtx *storage.Context, bundles map[string]*bundleApi.Bundle, legacy bool, runtimeRegoVersion ast.RegoVersion) error {
	params := storage.WriteParams
	params.Context = txnCtx

	for name, b := range bundles {
		if len(b.Raw) == 0 {
			// Write data from each new bundle into the store. Only write under the
			// roots contained in their manifest.
			data, ok := bjson.MustNew(b.Data).(bjson.Object)
			if !ok {
				return fmt.Errorf("corrupt bundle data")
			}

			if err := writeData(ctx, store, txn, *b.Manifest.Roots, data); err != nil {
				return err
			}

			for _, mf := range b.Modules {
				var path string

				// For backwards compatibility, in legacy mode, upsert policies to
				// the unprefixed path.
				if legacy {
					path = mf.Path
				} else {
					path = modulePathWithPrefix(name, mf.Path)
				}

				if err := store.UpsertPolicy(ctx, txn, path, mf.Raw); err != nil {
					return err
				}

				if err := writeModuleRegoVersionToStore(ctx, store, txn, b, mf, path, runtimeRegoVersion); err != nil {
					return err
				}
			}
		} else {
			params.BasePaths = *b.Manifest.Roots

			err := store.Truncate(ctx, txn, params, bundleApi.NewIterator(b.Raw))
			if err != nil {
				return fmt.Errorf("store truncate failed for bundle '%s': %v", name, err)
			}

			for _, f := range b.Raw {
				if strings.HasSuffix(f.Path, bundleApi.RegoExt) {
					p, err := getFileStoragePath(f.Path)
					if err != nil {
						return fmt.Errorf("failed get storage path for module '%s' in bundle '%s': %w", f.Path, name, err)
					}

					// HACK(philip): Because the upstream OPA bundle.Raw type
					// hides the module field from us, we have to synthesize our
					// own bundle.ModuleFile for each incoming Rego policy. This
					// works, because the logic that writes the Rego version to
					// the store will infer the correct Rego version for this
					// file.
					m := bundleApi.ModuleFile{Raw: f.Value, Path: f.Path}

					// 'f.module.Path' contains the module's path as it relates to the bundle root, and can be used for looking up the rego-version.
					// 'f.Path' can differ, based on how the bundle reader was initialized.
					if err := writeModuleRegoVersionToStore(ctx, store, txn, b, m, p.String(), runtimeRegoVersion); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

// Taken from OPA's v1/bundle/file.go:
func getFileStoragePath(path string) (storage.Path, error) {
	fpath := strings.TrimLeft(normalizePath(filepath.Dir(path)), "/.")
	if strings.HasSuffix(path, bundleApi.RegoExt) {
		fpath = strings.Trim(normalizePath(path), "/")
	}

	p, ok := storage.ParsePathEscaped("/" + fpath)
	if !ok {
		return nil, fmt.Errorf("storage path invalid: %v", path)
	}
	return p, nil
}

// Take from OPA's v1/bundle/bundle.go:
func normalizePath(p string) string {
	return filepath.ToSlash(p)
}

func writeData(ctx context.Context, store storage.Store, txn storage.Transaction, roots []string, data bjson.Object) error {
	for _, root := range roots {
		path, ok := storage.ParsePathEscaped("/" + root)
		if !ok {
			return fmt.Errorf("manifest root path invalid: %v", root)
		}
		if value, ok := lookup(path, data); ok {
			if len(path) > 0 {
				if err := storage.MakeDir(ctx, store, txn, path[:len(path)-1]); err != nil {
					return err
				}
			}
			if err := store.Write(ctx, txn, storage.AddOp, path, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func compileModules(compiler *ast.Compiler, m metrics.Metrics, bundles map[string]*bundleApi.Bundle, extraModules map[string]*ast.Module, legacy bool) error {
	m.Timer(metrics.RegoModuleCompile).Start()
	defer m.Timer(metrics.RegoModuleCompile).Stop()

	modules := map[string]*ast.Module{}

	// preserve any modules already on the compiler
	for name, module := range compiler.Modules {
		modules[name] = module
	}

	// preserve any modules passed in from the store
	for name, module := range extraModules {
		modules[name] = module
	}

	// include all the new bundle modules
	for bundleName, b := range bundles {
		if legacy {
			for _, mf := range b.Modules {
				modules[mf.Path] = mf.Parsed
			}
		} else {
			for name, module := range b.ParsedModules(bundleName) {
				x := strings.TrimLeft(name, "/")
				modules[x] = module
			}
		}
	}

	if compiler.Compile(modules); compiler.Failed() {
		return compiler.Errors
	}

	return nil
}

func lookup(path storage.Path, data bjson.Object) (interface{}, bool) {
	if len(path) == 0 {
		return data, true
	}

	if data == nil {
		return nil, false
	}

	for i := 0; i < len(path)-1; i++ {
		value := data.Value(path[i])
		if value == nil {
			return nil, false
		}

		if _, ok := value.(bjson.Object); !ok {
			return nil, false
		}
		data = value.(bjson.Object)
	}
	value := data.Value(path[len(path)-1])
	return value, value != nil
}

func hasRootsOverlap(ctx context.Context, store storage.Store, txn storage.Transaction, bundles map[string]*bundleApi.Bundle) error {
	collisions := map[string][]string{}
	allBundles, err := ReadBundleNamesFromStore(ctx, store, txn)
	if suppressNotFound(err) != nil {
		return err
	}

	allRoots := map[string][]string{}

	// Build a map of roots for existing bundles already in the system
	for _, name := range allBundles {
		roots, err := ReadBundleRootsFromStore(ctx, store, txn, name)
		if suppressNotFound(err) != nil {
			return err
		}
		allRoots[name] = roots
	}

	// Add in any bundles that are being activated, overwrite existing roots
	// with new ones where bundles are in both groups.
	for name, bundle := range bundles {
		allRoots[name] = *bundle.Manifest.Roots
	}

	// Now check for each new bundle if it conflicts with any of the others
	for name, bundle := range bundles {
		for otherBundle, otherRoots := range allRoots {
			if name == otherBundle {
				// Skip the current bundle being checked
				continue
			}

			// Compare the "new" roots with other existing (or a different bundles new roots)
			for _, newRoot := range *bundle.Manifest.Roots {
				for _, otherRoot := range otherRoots {
					if bundleApi.RootPathsOverlap(newRoot, otherRoot) {
						collisions[otherBundle] = append(collisions[otherBundle], newRoot)
					}
				}
			}
		}
	}

	if len(collisions) > 0 {
		var bundleNames []string
		for name := range collisions {
			bundleNames = append(bundleNames, name)
		}
		return fmt.Errorf("detected overlapping roots in bundle manifest with: %s", bundleNames)
	}
	return nil
}

func applyPatches(ctx context.Context, store storage.Store, txn storage.Transaction, patches []bundleApi.PatchOperation) error {
	for _, pat := range patches {

		// construct patch path
		path, ok := patch.ParsePatchPathEscaped("/" + strings.Trim(pat.Path, "/"))
		if !ok {
			return fmt.Errorf("error parsing patch path")
		}

		var op storage.PatchOp
		switch pat.Op {
		case "upsert":
			op = storage.AddOp

			_, err := store.Read(ctx, txn, path[:len(path)-1])
			if err != nil {
				if !storage.IsNotFound(err) {
					return err
				}

				if err := storage.MakeDir(ctx, store, txn, path[:len(path)-1]); err != nil {
					return err
				}
			}
		case "remove":
			op = storage.RemoveOp
		case "replace":
			op = storage.ReplaceOp
		default:
			return fmt.Errorf("bad patch operation: %v", pat.Op)
		}

		// apply the patch
		if err := store.Write(ctx, txn, op, path, bjson.MustNew(pat.Value)); err != nil {
			return err
		}
	}

	return nil
}

func modulePathWithPrefix(bundleName string, modulePath string) string {
	// Default prefix is just the bundle name
	prefix := bundleName

	// Bundle names are sometimes just file paths, some of which
	// are full urls (file:///foo/). Parse these and only use the path.
	parsed, err := url.Parse(bundleName)
	if err == nil {
		prefix = filepath.Join(parsed.Host, parsed.Path)
	}

	return filepath.Join(prefix, modulePath)
}

func equalWasmResolversAndRoots(a, b bundleApi.Manifest) bool {
	if len(a.WasmResolvers) != len(b.WasmResolvers) {
		return false
	}

	for i := 0; i < len(a.WasmResolvers); i++ {
		if !a.WasmResolvers[i].Equal(&b.WasmResolvers[i]) {
			return false
		}
	}

	return checkIfRootsEqual(*a.Roots, *b.Roots)
}

func checkIfRootsEqual(a, b []string) bool {
	return rootSet(a).Equal(rootSet(b))
}

func rootSet(roots []string) stringSet {
	rs := map[string]struct{}{}

	for _, r := range roots {
		rs[r] = struct{}{}
	}

	return stringSet(rs)
}

type stringSet map[string]struct{}

func (ss stringSet) Equal(other stringSet) bool {
	if len(ss) != len(other) {
		return false
	}
	for k := range other {
		if _, ok := ss[k]; !ok {
			return false
		}
	}
	return true
}

func getNormalizedPath(path string) []string {
	// Remove leading / and . characters from the directory path. If the bundle
	// was written with OPA then the paths will contain a leading slash. On the
	// other hand, if the path is empty, filepath.Dir will return '.'.
	// Note: filepath.Dir can return paths with '\' separators, always use
	// filepath.ToSlash to keep them normalized.
	dirpath := strings.TrimLeft(filepath.ToSlash(filepath.Dir(path)), "/.")
	var key []string
	if dirpath != "" {
		key = strings.Split(dirpath, "/")
	}
	return key
}

// Helpers for the older single (unnamed) bundle style manifest storage.

// LegacyManifestStoragePath is the older unnamed bundle path for manifests to be stored.
// Deprecated: Use ManifestStoragePath and named bundles instead.
var (
	legacyManifestStoragePath = storage.MustParsePath("/system/bundle/manifest")
	legacyRevisionStoragePath = append(legacyManifestStoragePath, "revision")
)

// LegacyWriteManifestToStore will write the bundle manifest to the older single (unnamed) bundle manifest location.
// Deprecated: Use WriteManifestToStore and named bundles instead.
func LegacyWriteManifestToStore(ctx context.Context, store storage.Store, txn storage.Transaction, manifest bundleApi.Manifest) error {
	return write(ctx, store, txn, legacyManifestStoragePath, bjson.MustNew(manifest))
}

// LegacyEraseManifestFromStore will erase the bundle manifest from the older single (unnamed) bundle manifest location.
// Deprecated: Use WriteManifestToStore and named bundles instead.
func LegacyEraseManifestFromStore(ctx context.Context, store storage.Store, txn storage.Transaction) error {
	err := store.Write(ctx, txn, storage.RemoveOp, legacyManifestStoragePath, bjson.MustNew(nil))
	if err != nil {
		return err
	}
	return nil
}

// LegacyReadRevisionFromStore will read the bundle manifest revision from the older single (unnamed) bundle manifest location.
// Deprecated: Use ReadBundleRevisionFromStore and named bundles instead.
func LegacyReadRevisionFromStore(ctx context.Context, store storage.Store, txn storage.Transaction) (string, error) {
	return readRevisionFromStore(ctx, store, txn, legacyRevisionStoragePath)
}
