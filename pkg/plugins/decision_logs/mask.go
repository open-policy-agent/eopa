// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package decisionlogs

import (
	"fmt"
	"strings"

	"github.com/Jeffail/gabs/v2"
)

func maskEvent(ruleset any, event map[string]any) (map[string]any, error) {
	rules, ok := ruleset.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected mask result: %v %[1]T", ruleset)
	}
	var err error
	masked := gabs.Wrap(event)
	for _, r := range rules {
		switch r := r.(type) {
		case string:
			masked, err = removeOne(masked, gabs.Wrap(r))
		case map[string]any:
			masked, err = evalMask(masked, r)
		}
		if err != nil {
			return nil, err
		}
	}
	return masked.Data().(map[string]any), nil
}

func removeOne(event, path *gabs.Container) (*gabs.Container, error) {
	return remove(event, path)
}

func evalMask(event *gabs.Container, mask map[string]any) (*gabs.Container, error) {
	m := gabs.Wrap(mask)
	switch m.S("op").Data().(string) {
	case "remove":
		return remove(event, m.S("path"))
	case "upsert":
		return upsert(event, m.S("path"), m.S("value"))
	}
	return nil, nil
}

func remove(ev, path *gabs.Container) (*gabs.Container, error) {
	ptr, err := ptr(path)
	if err != nil {
		return nil, err
	}
	if err := ev.Delete(ptr...); err != nil {
		switch err {
		case gabs.ErrNotObjOrArray, gabs.ErrNotFound:
			return ev, nil
		}
		return nil, err
	}
	if err := ev.ArrayAppend(path.Data().(string), "erased"); err != nil {
		return nil, err
	}
	return ev, nil
}

func upsert(ev, path, value *gabs.Container) (*gabs.Container, error) {
	ptr, err := ptr(path)
	if err != nil {
		return nil, err
	}
	if !ev.Exists(ptr[0]) { // check if top-level key ("input", "result", "nd_builtin_cache") exists
		return gabs.New(), nil
	}
	if _, err := ev.Set(value.Data(), ptr...); err != nil {
		switch {
		case err == gabs.ErrPathCollision:
			return ev, nil
		case strings.HasPrefix(err.Error(), "failed to resolve path segment"):
			return ev, nil
		}
		return nil, err
	}
	if err := ev.ArrayAppend(path.Data().(string), "masked"); err != nil {
		return nil, err
	}
	return ev, nil
}

func ptr(path *gabs.Container) ([]string, error) {
	str, ok := path.Data().(string)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T", path.Data())
	}
	// "/foo/bar.baz" ~> "/foo/bar~1baz" ~> ".foo.bar~1baz"
	p := strings.ReplaceAll(
		strings.ReplaceAll(str,
			".",
			"~1"),
		"/",
		".")
	sl := gabs.DotPathToSlice(p[1:]) // drop leading "."
	switch sl[0] {
	case "input", "result", "nd_builtin_cache": // OK
	default:
		return nil, fmt.Errorf(`invalid mask pointer: %s, must be one of "input", "result" or "nd_builtin_cache"`, sl[0])
	}
	return sl, nil
}
