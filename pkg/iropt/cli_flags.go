// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package iropt

import "reflect"

// NOTE(philip): We use struct tags to encode the CLI-visible names for each pass.
// Ref: https://stackoverflow.com/a/30889373
type OptimizationPassFlags struct {
	LoopInvariantCodeMotion bool `cli:"licm"`
}

// HACK(philip): Horrible reflection hack, thankfully only needed to make
// CLI enable/disable flags easier to work with.
// Ref: https://stackoverflow.com/a/18931036
func (opf *OptimizationPassFlags) SetFlag(name string, value bool) {
	r := reflect.ValueOf(opf)
	reflect.Indirect(r).FieldByName(name).SetBool(value)
}

func (opf *OptimizationPassFlags) GetFlag(name string) bool {
	r := reflect.ValueOf(opf)
	return reflect.Indirect(r).FieldByName(name).Bool()
}

// HACK(philip): More horrific reflection hackery, also for the CLI.
// Ref: https://stackoverflow.com/a/29185381
func (opf *OptimizationPassFlags) GetFieldPtrMapping() map[string]*bool {
	val := reflect.Indirect(reflect.ValueOf(opf))

	boolFieldMap := make(map[string]*bool)

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		if field.Kind() == reflect.Bool {
			fieldName := val.Type().Field(i).Name
			boolPtr := field.Addr().Interface().(*bool)
			boolFieldMap[fieldName] = boolPtr
		}
	}

	return boolFieldMap
}

// Ref: https://stackoverflow.com/a/50208292
func (opf *OptimizationPassFlags) GetFieldNames() []string {
	r := reflect.ValueOf(opf).Elem()
	numFields := r.NumField()
	out := make([]string, 0, numFields)
	for i := 0; i < numFields; i++ {
		out = append(out, r.Type().Field(i).Name)
	}
	return out
}

// HACK(philip): Relies on pass names and CLI flags being the same length.
func (opf *OptimizationPassFlags) GetFlagToFieldsMapping() map[string]string {
	fieldNames := opf.GetFieldNames()
	flagNames := opf.GetCLIFlagNames()
	out := make(map[string]string, len(flagNames))
	for i := 0; i < len(flagNames); i++ {
		field := fieldNames[i]
		flag := flagNames[i]
		out[flag] = field
	}
	return out
}

// Ref: https://stackoverflow.com/a/30889373
func (opf *OptimizationPassFlags) GetCLIFlagNames() []string {
	r := reflect.ValueOf(opf).Elem()
	numFields := r.NumField()
	out := make([]string, 0, numFields)
	for i := 0; i < numFields; i++ {
		out = append(out, r.Type().Field(i).Tag.Get("cli"))
	}
	return out
}

func (opf *OptimizationPassFlags) MergeDisables(other *OptimizationPassFlags) *OptimizationPassFlags {
	out := *opf
	for _, f := range opf.GetFieldNames() {
		if other.GetFlag(f) {
			out.SetFlag(f, false)
		}
	}
	return &out
}

func (opf *OptimizationPassFlags) MergeEnables(other *OptimizationPassFlags) *OptimizationPassFlags {
	out := *opf
	for _, f := range opf.GetFieldNames() {
		if other.GetFlag(f) {
			out.SetFlag(f, true)
		}
	}
	return &out
}
