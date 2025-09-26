// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package compile

import (
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/v1/ast"
)

const invalidUnknownCode = "invalid_unknowns_annotation"

func ExtractUnknownsFromAnnotations(comp *ast.Compiler, ref ast.Ref) ([]ast.Ref, []*ast.Error) {
	// find ast.Rule for ref
	rules := comp.GetRulesExact(ref)
	if len(rules) == 0 {
		return nil, nil
	}
	rule := rules[0] // rule scope doesn't make sense here, so it doesn't matter which rule we use
	return unknownsFromAnnotationsSet(comp.GetAnnotationSet(), rule)
}

func unknownsFromAnnotationsSet(as *ast.AnnotationSet, rule *ast.Rule) ([]ast.Ref, []*ast.Error) {
	if as == nil {
		return nil, nil
	}
	var unknowns []ast.Ref
	var errs []*ast.Error

	for _, ar := range as.Chain(rule) {
		ann := ar.Annotations
		if ann == nil || ann.Compile == nil {
			continue
		}
		unkArray := ann.Compile.Unknowns
		for _, ref := range unkArray {
			if ref.HasPrefix(ast.DefaultRootRef) || ref.HasPrefix(ast.InputRootRef) {
				unknowns = append(unknowns, ref)
			} else {
				errs = append(errs, ast.NewError(invalidUnknownCode, ann.Loc(), "unknowns must be prefixed with `input` or `data`: %v", ref))
			}
		}
	}

	return unknowns, errs
}

func ExtractMaskRuleRefFromAnnotations(comp *ast.Compiler, ref ast.Ref) (ast.Ref, *ast.Error) {
	// find ast.Rule for ref
	rules := comp.GetRulesExact(ref)
	if len(rules) == 0 {
		return nil, nil
	}
	rule := rules[0] // rule scope doesn't make sense here, so it doesn't matter which rule we use
	return maskRuleFromAnnotationsSet(comp.GetAnnotationSet(), rule)
}

func maskRuleFromAnnotationsSet(as *ast.AnnotationSet, rule *ast.Rule) (ast.Ref, *ast.Error) {
	if as == nil {
		return nil, nil
	}

	for _, ar := range as.Chain(rule) {
		ann := ar.Annotations
		if ann == nil || ann.Compile == nil {
			continue
		}
		if maskRule := ann.Compile.MaskRule; maskRule != nil {
			if !maskRule.HasPrefix(ast.DefaultRootRef) {
				// If the mask_rule is not a data ref, add package prefix.
				maskRule = rule.Module.Package.Path.Extend(maskRule)
			}
			return maskRule, nil
		}
	}

	return nil, nil // No mask rule found.
}

func ShortsFromMappings(mappings map[string]any) Set[string] {
	shorts := NewSet[string]()
	for _, mapping := range mappings {
		m, ok := mapping.(map[string]any)
		if !ok {
			continue
		}
		for n, nmap := range m {
			m, ok := nmap.(map[string]any)
			if !ok {
				continue
			}
			if _, ok := m["$table"]; ok {
				shorts = shorts.Add(n)
			}
		}
	}
	return shorts
}

type aerrs struct {
	errs []*ast.Error
}

func FromASTErrors(errs ...*ast.Error) error {
	return &aerrs{errs}
}

func (es *aerrs) Error() string {
	s := strings.Builder{}
	if x := len(es.errs); x > 1 {
		fmt.Fprintf(&s, "%d errors occurred during compilation:\n", x)
	} else {
		s.WriteString("1 error occurred during compilation:\n")
	}
	for i := range es.errs {
		s.WriteString(es.errs[i].Error())
	}
	return s.String()
}
