package json

import (
	"bytes"
	"errors"
	"fmt"
)

type Patch []Op

type PatchOp string

const (
	PatchOpAdd     PatchOp = "add"
	PatchOpReplace PatchOp = "replace"
	PatchOpRemove  PatchOp = "remove"
	PatchOpTest    PatchOp = "test"
	PatchOpCopy    PatchOp = "copy"
	PatchOpMove    PatchOp = "move"
	PatchOpCreate  PatchOp = "create"
)

type Op struct {
	Op          PatchOp `json:"op"`
	Path        string  `json:"path"`
	Value       Json    `json:"value,omitempty"`
	From        string  `json:"from"` // Note, only marshaled if operation is either copy or move.
	valueBinary File    // For package internal use only.
}

type JsonPatchSpec []map[string]interface{}

func NewPatch(spec JsonPatchSpec) (Patch, error) {
	patch := make(Patch, 0, len(spec))
	for _, m := range spec {
		op, err := newOpFromMap(m)
		if err != nil {
			return nil, err
		}
		patch = append(patch, op)
	}
	return patch, nil
}

func newOpFromMap(spec map[string]interface{}) (Op, error) {
	op, ok := spec["op"]
	if !ok {
		return Op{}, errors.New("json patch: op is missing")
	}
	opStr, ok := op.(string)
	if !ok {
		return Op{}, errors.New("json patch: op is not a string")
	}

	path, ok := spec["path"]
	if !ok {
		return Op{}, errors.New("json patch: path is missing")
	}
	pathStr, ok := path.(string)
	if !ok {
		return Op{}, errors.New("json patch: path is not a string")
	}
	res := Op{Op: PatchOp(opStr), Path: pathStr}

	switch res.Op {
	case PatchOpAdd, PatchOpReplace, PatchOpTest, PatchOpCreate:
		value, ok := spec["value"]
		if !ok {
			return Op{}, errors.New("json patch: value is missing")
		}
		var jvalue Json
		if jvalue, ok = value.(Json); !ok {
			var err error
			jvalue, err = New(value)
			if err != nil {
				return Op{}, fmt.Errorf("invalid json patch value: %w", err)
			}
		}
		res.Value = jvalue
	case PatchOpRemove:
		// nothing
	case PatchOpCopy, PatchOpMove:
		from, ok := spec["from"]
		if !ok {
			return Op{}, errors.New("json patch: from is missing")
		}
		fromStr, ok := from.(string)
		if !ok {
			return Op{}, errors.New("json patch: from is not a string")
		}
		res.From = fromStr
	default:
		return Op{}, fmt.Errorf("json patch: invalid op %q", res.Op)

	}
	return res, nil
}

func (p *Patch) UnmarshalJSON(data []byte) error {
	return p.Unmarshal(NewDecoder(bytes.NewReader(data)))
}

func (p *Patch) Unmarshal(decoder *Decoder) error {
	*p = make([]Op, 0)
	return decoder.UnmarshalArray(func(decoder *Decoder) error {
		opData := map[string]interface{}{}

		if err := decoder.UnmarshalObject(func(property string, decoder *Decoder) error {
			var err error
			switch property {
			case "op":
				var verb string
				verb, err = decoder.UnmarshalString()
				if err == nil {
					opData["op"] = verb
				}

			case "path":
				var path string
				path, err = decoder.UnmarshalString()
				if err == nil {
					opData["path"] = path
				}

			case "value":
				var value interface{}
				value, err = decoder.Decode()
				if err == nil {
					opData["value"] = value
				}
			case "from":
				var from string
				from, err = decoder.UnmarshalString()
				if err == nil {
					opData["from"] = from
				}
			}
			return err
		}); err != nil {
			return err
		}

		op, err := newOpFromMap(opData)
		if err != nil {
			return err
		}

		*p = append(*p, op)

		return nil
	})
}

type JsonPatchTestFailed struct {
	path string
}

var _ error = JsonPatchTestFailed{}

func (e JsonPatchTestFailed) Error() string {
	return fmt.Sprintf("JSON patch test failed for path %s", e.path)
}

func (p Patch) ApplyTo(doc Json) (Json, error) {
	for _, op := range p {
		var err error
		doc, err = op.ApplyTo(doc)
		if err != nil {
			return nil, err
		}
	}

	return doc, nil
}

func (o Op) MarshalJSON() ([]byte, error) {
	m := make(map[string]File, 4)
	m["op"] = NewString(string(o.Op))
	m["path"] = NewString(o.Path)
	if o.Value != nil {
		m["value"] = o.Value
	}
	if o.Op == PatchOpCopy || o.Op == PatchOpMove {
		m["from"] = NewString(o.From)
	}
	obj := NewObject(m)

	var b bytes.Buffer
	if _, err := obj.WriteTo(&b); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func (o Op) ApplyTo(file Json) (Json, error) {
	doc, err := o.applyTo(file)
	if err != nil {
		return nil, err
	}

	j, ok := doc.(Json)
	if !ok {
		return nil, fmt.Errorf("json patch: binary result")
	}

	return j, nil
}

func (o Op) applyTo(file File) (File, error) {
	switch o.Op {
	case PatchOpCopy:
		if _, ok := file.(Json); !ok {
			return nil, fmt.Errorf("json patch: binary copy")
		}

		value, err := file.(Json).Extract(o.From)
		if err != nil {
			return nil, err
		}
		return Op{Op: PatchOpAdd, Value: value, Path: o.Path}.applyTo(file)

	case PatchOpMove:
		if _, ok := file.(Json); !ok {
			return nil, fmt.Errorf("json patch: binary move")
		}

		value, err := Op{Op: PatchOpCopy, From: o.From, Path: o.Path}.applyTo(file)
		if err != nil {
			return nil, err
		}
		if o.From != o.Path {
			return Op{Op: PatchOpRemove, Path: o.From}.applyTo(value)
		}
		return value, nil

	case PatchOpAdd, PatchOpRemove, PatchOpReplace, PatchOpTest, PatchOpCreate:
		p, err := preparePointer(o.Path)
		if err != nil {
			return nil, err
		}

		value := o.valueBinary
		if value == nil {
			value = o.Value
		}

		res, err := apply(file, p, value, o.Op)
		if err != nil {
			var e JsonPatchTestFailed
			if errors.As(err, &e) {
				return nil, JsonPatchTestFailed{path: o.Path}
			}
			return nil, err
		}

		return res, nil
	}

	return nil, fmt.Errorf("json patch: invalid operation %s", o.Op)
}

func apply(json File, ptr []string, value File, op PatchOp) (File, error) {
	if json == nil {
		return nil, fmt.Errorf("json patch: not found")
	}
	if len(ptr) == 0 {
		switch op {
		case PatchOpRemove:
			return nil, nil
		case PatchOpAdd, PatchOpReplace, PatchOpCreate:
			return value, nil
		case PatchOpTest:
			a, ok := json.(Json)
			if !ok {
				return nil, fmt.Errorf("json patch: binary")
			}

			b, ok := value.(Json)
			if !ok {
				return nil, fmt.Errorf("json patch: binary")
			}

			if a.Compare(b) != 0 {
				return nil, JsonPatchTestFailed{}
			}
			return json, nil
		default:
			return nil, fmt.Errorf("json patch: invalid operation %s", op)
		}
	}

	field := ptr[0]
	switch v := (json).(type) {
	case Object:
		if len(ptr) == 1 {
			switch op {
			case PatchOpRemove:
				if v.valueImpl(field) == nil {
					return nil, fmt.Errorf("json patch: key %s is not found", field)
				}
				v = v.Clone(false).(Object)
				v = v.Remove(field)
			case PatchOpAdd, PatchOpCreate:
				v = v.Clone(false).(Object)
				v, _ = v.setImpl(field, value)
			case PatchOpReplace:
				if m := v.valueImpl(field); m == nil {
					return nil, fmt.Errorf("json patch: object member '%v' not found", ptr[0])
				}
				v = v.Clone(false).(Object)
				v, _ = v.setImpl(field, value)
			case PatchOpTest:
				m := v.Value(field)
				if m == nil {
					return nil, fmt.Errorf("json patch: object member '%v' not found", ptr[0])
				}

				b, ok := value.(Json)
				if !ok {
					return nil, fmt.Errorf("json patch: binary")
				}

				if m.Compare(b) != 0 {
					return nil, JsonPatchTestFailed{}
				}

			default:
				return nil, fmt.Errorf("json patch: invalid operation %s", op)
			}
			return v, nil
		}
		child := v.Value(field)
		if child == nil {
			if op == PatchOpCreate {
				child = NewObject(nil)

				v = v.Clone(false).(Object)
				v, _ = v.setImpl(field, child)
			} else {
				return nil, fmt.Errorf("json patch: key %s is not found", field)
			}
		}
		sv, err := apply(child, ptr[1:], value, op)
		if err != nil {
			return nil, err
		}
		v = v.Clone(false).(Object)
		v, _ = v.setImpl(field, sv)
		return v, nil

	case Array:
		var err error
		var index int
		if field == "-" {
			index = v.Len()
		} else if index, err = parseInt(field); err != nil {
			return nil, fmt.Errorf("json patch: invalid array index '%s'", field)
		} else if len(field) > 1 && field[0] == '0' {
			return nil, fmt.Errorf("json patch: leading zeroes are not allowed in array index %s", field)
		}
		if index < 0 || (op == PatchOpAdd || op == PatchOpCreate) && index > v.Len() || (op != PatchOpAdd && op != PatchOpCreate) && index >= v.Len() {
			return nil, fmt.Errorf("json: invalid array index %s", field)
		}

		if len(ptr) == 1 {
			switch op {
			case PatchOpRemove:
				v = v.Clone(false).(Array)
				v = v.RemoveIdx(index).(Array)
			case PatchOpReplace:
				v = v.Clone(false).(Array)
				v = v.SetIdx(index, value).(Array)
			case PatchOpAdd, PatchOpCreate:
				if index == v.Len() {
					v = v.Clone(false).(Array)
					v = v.Append(value)
				} else {
					x := make([]File, v.Len()+1)
					for i, j := 0, 0; i < len(x); i, j = i+1, j+1 {
						if i == index {
							x[i] = value
							j--
						} else {
							x[i] = v.Value(j)
						}
					}
					v = NewArray(x...)
				}
			case PatchOpTest:
				b, ok := value.(Json)
				if !ok {
					return nil, errors.New("json patch: binary")
				}

				if v.Value(index).Compare(b) != 0 {
					return nil, JsonPatchTestFailed{}
				}
			default:
				return nil, fmt.Errorf("json patch: invalid operation %s", op)
			}

			return v, nil
		}
		sv, err := apply(v.Value(index), ptr[1:], value, op)
		if err != nil {
			return nil, err
		}
		v = v.Clone(false).(Array)
		v = v.SetIdx(index, sv).(Array)
		return v, nil
	}

	return nil, fmt.Errorf("json patch: unsupported type '%T'", json)
}
