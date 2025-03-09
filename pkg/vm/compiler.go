package vm

import (
	"fmt"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/opa/v1/topdown"
)

type (
	Compiler struct {
		policy        *ir.Policy
		functionIndex map[string]int
		builtinFuncs  map[string]*topdown.Builtin
	}
)

const (
	memberSF = iota
	memberWithKeySF
	objectGetSF
	objectKeysSF
	objectRemoveSF
	objectFilterSF
	objectUnionSF
	concatSF
	endsWithSF
	startsWithSF
	sprintfSF
	arrayConcatSF
	arraySliceSF
	countSF
	walkBuiltinSF
	equalSF
	notEqualSF
	orSF
	isArraySF
	isStringSF
	isBooleanSF
	isObjectSF
	isSetSF
	isNumberSF
	isNullSF
	jsonUnmarshalSF
	typeNameBuiltinSF
	numbersRangeSF
	numbersRangeStepSF
	globMatchSF
)

var specializedBuiltins = map[string]uint32{
	ast.Member.Name:           memberSF,
	ast.MemberWithKey.Name:    memberWithKeySF,
	ast.ObjectGet.Name:        objectGetSF,
	ast.ObjectKeys.Name:       objectKeysSF,
	ast.ObjectRemove.Name:     objectRemoveSF,
	ast.ObjectFilter.Name:     objectFilterSF,
	ast.ObjectUnion.Name:      objectUnionSF,
	ast.Concat.Name:           concatSF,
	ast.EndsWith.Name:         endsWithSF,
	ast.StartsWith.Name:       startsWithSF,
	ast.Sprintf.Name:          sprintfSF,
	ast.ArrayConcat.Name:      arrayConcatSF,
	ast.ArraySlice.Name:       arraySliceSF,
	ast.Count.Name:            countSF,
	ast.WalkBuiltin.Name:      walkBuiltinSF,
	ast.Equal.Name:            equalSF,
	ast.NotEqual.Name:         notEqualSF,
	ast.Or.Name:               orSF,
	ast.IsArray.Name:          isArraySF,
	ast.IsString.Name:         isStringSF,
	ast.IsBoolean.Name:        isBooleanSF,
	ast.IsObject.Name:         isObjectSF,
	ast.IsSet.Name:            isSetSF,
	ast.IsNumber.Name:         isNumberSF,
	ast.IsNull.Name:           isNullSF,
	ast.JSONUnmarshal.Name:    jsonUnmarshalSF,
	ast.TypeNameBuiltin.Name:  typeNameBuiltinSF,
	ast.NumbersRange.Name:     numbersRangeSF,
	ast.NumbersRangeStep.Name: numbersRangeStepSF,
	ast.GlobMatch.Name:        globMatchSF,
}

var specializedBuiltinsByNum = [...]func(*State, []Value) error{
	memberSF:           memberBuiltin,
	memberWithKeySF:    memberWithKeyBuiltin,
	objectGetSF:        objectGetBuiltin,
	objectKeysSF:       objectKeysBuiltin,
	objectRemoveSF:     objectRemoveBuiltin,
	objectFilterSF:     objectFilterBuiltin,
	objectUnionSF:      objectUnionBuiltin,
	concatSF:           stringsConcatBuiltin,
	endsWithSF:         stringsEndsWithBuiltin,
	startsWithSF:       stringsStartsWithBuiltin,
	sprintfSF:          stringsSprintfBuiltin,
	arrayConcatSF:      arrayConcatBuiltin,
	arraySliceSF:       arraySliceBuiltin,
	countSF:            countBuiltin,
	walkBuiltinSF:      walkBuiltin,
	equalSF:            equalBuiltin,
	notEqualSF:         notEqualBuiltin,
	orSF:               binaryOrBuiltin,
	isArraySF:          typeSpecializedBuiltinFunc(typeArray),
	isStringSF:         typeSpecializedBuiltinFunc(typeString),
	isBooleanSF:        typeSpecializedBuiltinFunc(typeBoolean),
	isObjectSF:         typeSpecializedBuiltinFunc(typeObject),
	isSetSF:            typeSpecializedBuiltinFunc(typeSet),
	isNumberSF:         typeSpecializedBuiltinFunc(typeNumber),
	isNullSF:           typeSpecializedBuiltinFunc(typeNull),
	jsonUnmarshalSF:    jsonUnmarshalBuiltin,
	typeNameBuiltinSF:  typenameBuiltin,
	numbersRangeSF:     numbersRangeBuiltin,
	numbersRangeStepSF: numbersRangeStepBuiltin,
	globMatchSF:        globMatchBuiltin,
	// ...
	31: nil,
}

func typeSpecializedBuiltinFunc(chk func(Value) bool) func(*State, []Value) error {
	return func(state *State, args []Value) error {
		return isTypeBuiltin(state, args, chk)
	}
}

func NewCompiler() *Compiler {
	return &Compiler{functionIndex: make(map[string]int)}
}

func (c *Compiler) WithPolicy(policy *ir.Policy) *Compiler {
	c.policy = policy
	return c
}

func (c *Compiler) WithBuiltins(bis map[string]*topdown.Builtin) *Compiler {
	c.builtinFuncs = bis
	return c
}

// Compile turns the IR into VM executable instructions
func (c *Compiler) Compile() (Executable, error) {
	strings, err := c.compileStrings()
	if err != nil {
		return Executable{}, err
	}

	functions, err := c.compileFuncs()
	if err != nil {
		return Executable{}, err
	}

	plans, err := c.compilePlans()
	if err != nil {
		return Executable{}, err
	}

	return Executable{}.Write(strings, functions, plans), nil
}

func (c *Compiler) compileStrings() ([]byte, error) {
	ss := make([]string, len(c.policy.Static.Strings))

	for i, s := range c.policy.Static.Strings {
		ss[i] = s.Value
	}

	return strings{}.Write(ss), nil
}

func (c *Compiler) compilePlans() ([]byte, error) {
	p := make([][]byte, 0, len(c.policy.Plans.Plans))

	for _, plan := range c.policy.Plans.Plans {
		data, err := c.compilePlan(plan)
		if err != nil {
			return nil, err
		}

		p = append(p, data)
	}

	return plans{}.Write(p), nil
}

func (c *Compiler) compilePlan(p *ir.Plan) ([]byte, error) {
	data, err := c.compileBlocks(p.Blocks)
	if err != nil {
		return nil, err
	}

	return plan{}.Write(p.Name, data), nil
}

func (c *Compiler) compileFuncs() ([]byte, error) {
	n := len(c.policy.Static.BuiltinFuncs) + len(c.policy.Funcs.Funcs)
	functions := make([]byte, 0, 4+n*4)
	functions = appendUint32(functions, uint32(n))

	foffsets := uint32(len(functions))
	functions = appendOffsetIndex(functions, n)

	for i, decl := range c.policy.Static.BuiltinFuncs {
		var builtinImpl topdown.BuiltinFunc
		var relation bool

		tbi, ok := c.builtinFuncs[decl.Name]
		if ok {
			builtinImpl = tbi.Func
			relation = tbi.Decl.Relation
		} else {
			builtinImpl = topdown.GetBuiltin(decl.Name)
			for _, f := range ast.Builtins {
				if f.Name == decl.Name {
					relation = f.Relation
				}
			}
		}
		if builtinImpl == nil {
			return nil, fmt.Errorf("builtin not found: %s", decl.Name)
		}

		c.functionIndex[decl.Name] = i

		offset := uint32(len(functions))
		putOffsetIndex(functions, foffsets, i, offset)

		// Encode the built-in
		if num, ok := specializedBuiltins[decl.Name]; ok {
			functions = append(functions, specializedBuiltin{}.Write(num)...)
		}
		if decl.Name == "rego.compile" {
			functions = append(functions, specializedBuiltinRegoCompile{}.Write()...)
		}

		functions = append(functions, builtin{}.Write(decl.Name, relation)...)
	}

	for i, fn := range c.policy.Funcs.Funcs {
		offset := uint32(len(functions))
		putOffsetIndex(functions, foffsets, i+len(c.policy.Static.BuiltinFuncs), offset)

		// Encode the function

		data, err := c.compileFunc(fn, i+len(c.policy.Static.BuiltinFuncs))
		if err != nil {
			return nil, fmt.Errorf("func %v: %w", fn.Name, err)
		}

		c.functionIndex[fn.Name] = i + len(c.policy.Static.BuiltinFuncs)
		functions = append(functions, data...)
	}

	return functions, nil
}

func (c *Compiler) compileFunc(fn *ir.Func, index int) ([]byte, error) {
	if len(fn.Params) == 0 {
		return nil, fmt.Errorf("illegal function: zero args")
	}

	params := make([]Local, 0, len(fn.Params))
	for i := range fn.Params {
		params = append(params, c.local(fn.Params[i]))
	}

	data, err := c.compileBlocks(fn.Blocks)
	if err != nil {
		return nil, err
	}

	ret := c.local(fn.Return)
	return function{}.Write(fn.Name, index, params, ret, data, fn.Path), nil
}

func (c *Compiler) compileBlocks(input []*ir.Block) ([]byte, error) {
	writtenBlocks := make([][]byte, 0, len(input))

	for i := range input {
		data, err := c.compileBlock(input[i])
		if err != nil {
			return nil, fmt.Errorf("block %d: %w", i, err)
		}

		writtenBlocks = append(writtenBlocks, data)
	}

	return blocks{}.Write(writtenBlocks), nil
}

func (c *Compiler) compileBlock(b *ir.Block) ([]byte, error) {
	datas := make([][]byte, 0, len(b.Stmts))

	for _, stmt := range b.Stmts {
		var data []byte

		switch stmt := stmt.(type) {
		// no-op

		case *ir.NopStmt:
			data = nop{}.Write()

		// local variable setters

		case *ir.AssignIntStmt:
			target := c.local(stmt.Target)
			data = assignInt{}.Write(stmt.Value, target)

		case *ir.AssignVarOnceStmt:
			source, target := c.localOrConst(stmt.Source), c.local(stmt.Target)
			data = assignVarOnce{}.Write(source, target)

		case *ir.AssignVarStmt:
			source, target := c.localOrConst(stmt.Source), c.local(stmt.Target)
			data = assignVar{}.Write(source, target)

		// looping and control flow

		case *ir.ScanStmt:
			var err error

			data, err = c.compileBlock(stmt.Block)
			if err != nil {
				return nil, err
			}

			source, key, value := c.local(stmt.Source), c.local(stmt.Key), c.local(stmt.Value)
			data = scan{}.Write(source, key, value, data)

		case *ir.BlockStmt:
			var err error

			data, err = c.compileBlocks(stmt.Blocks)
			if err != nil {
				return nil, err
			}

			data = blockStmt{}.Write(data)

		case *ir.BreakStmt:
			data = breakStmt{}.Write(stmt.Index)

		case *ir.NotStmt:
			var err error

			data, err = c.compileBlock(stmt.Block)
			if err != nil {
				return nil, err
			}

			data = not{}.Write(data)

		case *ir.ReturnLocalStmt:
			source := c.local(stmt.Source)
			data = returnLocal{}.Write(source)

		// calls

		case *ir.CallDynamicStmt:
			var args []Local
			for _, arg := range stmt.Args {
				args = append(args, c.local(arg))
			}

			var path []LocalOrConst
			for _, seg := range stmt.Path {
				path = append(path, c.localOrConst(seg))
			}

			result := c.local(stmt.Result)
			data = callDynamic{}.Write(args, result, path)

		case *ir.CallStmt:
			i, ok := c.functionIndex[stmt.Func]
			if !ok {
				return nil, fmt.Errorf("function '%s' not found", stmt.Func)
			}

			var args []LocalOrConst
			for _, arg := range stmt.Args {
				args = append(args, c.localOrConst(arg))
			}

			result := c.local(stmt.Result)
			data = call{}.Write(i, args, result)

		// dot and comparison

		case *ir.DotStmt:
			source, key, target := c.localOrConst(stmt.Source), c.localOrConst(stmt.Key), c.local(stmt.Target)
			data = dot{}.Write(source, key, target)

		case *ir.EqualStmt:
			a, b := c.localOrConst(stmt.A), c.localOrConst(stmt.B)
			data = equal{}.Write(a, b)

		case *ir.NotEqualStmt:
			a, b := c.localOrConst(stmt.A), c.localOrConst(stmt.B)
			data = notEqual{}.Write(a, b)

		// type checks

		case *ir.IsArrayStmt:
			source := c.localOrConst(stmt.Source)
			data = isArray{}.Write(source)

		case *ir.IsSetStmt:
			source := c.localOrConst(stmt.Source)
			data = isSet{}.Write(source)

		case *ir.IsObjectStmt:
			source := c.localOrConst(stmt.Source)
			data = isObject{}.Write(source)

		case *ir.IsDefinedStmt:
			source := c.local(stmt.Source)
			data = isDefined{}.Write(source)

		case *ir.IsUndefinedStmt:
			source := c.local(stmt.Source)
			data = isUndefined{}.Write(source)

		// constructors

		case *ir.MakeNullStmt:
			target := c.local(stmt.Target)
			data = makeNull{}.Write(target)

		case *ir.MakeNumberIntStmt:
			target := c.local(stmt.Target)
			data = makeNumberInt{}.Write(stmt.Value, target)

		case *ir.MakeNumberRefStmt:
			target := c.local(stmt.Target)
			data = makeNumberRef{}.Write(stmt.Index, target)

		case *ir.MakeArrayStmt:
			target := c.local(stmt.Target)
			data = makeArray{}.Write(stmt.Capacity, target)

		case *ir.MakeSetStmt:
			target := c.local(stmt.Target)
			data = makeSet{}.Write(target)

		case *ir.MakeObjectStmt:
			target := c.local(stmt.Target)
			data = makeObject{}.Write(target)

		// collection operations

		case *ir.LenStmt:
			source, target := c.localOrConst(stmt.Source), c.local(stmt.Target)
			data = lenStmt{}.Write(source, target)

		case *ir.ArrayAppendStmt:
			value, array := c.localOrConst(stmt.Value), c.local(stmt.Array)
			data = arrayAppend{}.Write(value, array)

		case *ir.SetAddStmt:
			value, set := c.localOrConst(stmt.Value), c.local(stmt.Set)
			data = setAdd{}.Write(value, set)

		case *ir.ObjectInsertOnceStmt:
			key, value, object := c.localOrConst(stmt.Key), c.localOrConst(stmt.Value), c.local(stmt.Object)
			data = objectInsertOnce{}.Write(key, value, object)

		case *ir.ObjectInsertStmt:
			key, value, object := c.localOrConst(stmt.Key), c.localOrConst(stmt.Value), c.local(stmt.Object)
			data = objectInsert{}.Write(key, value, object)

		case *ir.ObjectMergeStmt:
			a, b, target := c.local(stmt.A), c.local(stmt.B), c.local(stmt.Target)
			data = objectMerge{}.Write(a, b, target)

		// with statements

		case *ir.WithStmt:
			var err error
			data, err = c.compileBlock(stmt.Block)
			if err != nil {
				return nil, err
			}

			local, value := c.local(stmt.Local), c.localOrConst(stmt.Value)
			data = with{}.Write(local, stmt.Path, value, data)

		// deprecated

		case *ir.ResultSetAddStmt: // deprecate: replace with set add
			value := c.local(stmt.Value)
			data = resultSetAdd{}.Write(value)

		case *ir.ResetLocalStmt: // deprecate: replace with assign int
			target := c.local(stmt.Target)
			data = resetLocal{}.Write(target)

		default:
			return nil, fmt.Errorf("unsupported statement type: %T", stmt)
		}

		datas = append(datas, data)
	}

	return block{}.Write(datas), nil
}

func (c *Compiler) local(l ir.Local) Local {
	return Local(l)
}

func (c *Compiler) localOrConst(l ir.Operand) LocalOrConst {
	switch v := l.Value.(type) {
	case ir.Local:
		return NewLocal(int(v))
	case *ir.Local:
		return NewLocal(int(*v))
	case ir.Bool:
		return NewBoolConst(bool(v))
	case *ir.Bool:
		return NewBoolConst(bool(*v))
	case ir.StringIndex:
		return NewStringIndexConst(int(v))
	case *ir.StringIndex:
		return NewStringIndexConst(int(*v))
	}

	panic(fmt.Sprintf("unsupported local or const: %T", l.Value))
}
