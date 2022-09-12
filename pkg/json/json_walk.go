package json

// Walker receives a stream of events in depth-first search order as a JSON object is traversed during the Json.Walk call.
type Walker interface {
	StartArray(d *DecodingState)
	EndArray(d *DecodingState, a Array)
	Boolean(d *DecodingState, b Bool)
	Nil(d *DecodingState)
	Number(d *DecodingState, f Float)
	StartObject(d *DecodingState)
	EndObject(d *DecodingState, o Object)
	String(d *DecodingState, s String)
}

func arrayWalk(a Array, state *DecodingState, walker Walker) {
	walker.StartArray(state)
	l := a.Len()

	if state == nil {
		for i := 0; i < l; i++ {
			a.Value(i).Walk(state, walker)
		}
	} else {
		for i := 0; i < l; i++ {
			state.PushIndex(i)
			a.Value(i).Walk(state, walker)
			state.Pop()
		}
	}
	walker.EndArray(state, a)
}

func objectWalk(o Object, state *DecodingState, walker Walker) {
	walker.StartObject(state)
	if state == nil {
		for _, name := range o.Names() {
			o.Value(name).Walk(state, walker)
		}
	} else {
		for _, name := range o.Names() {
			state.PushField(name)
			o.Value(name).Walk(state, walker)
			state.Pop()
		}
	}
	walker.EndObject(state, o)
}

// DecodingState holds the current state of traversal.
type DecodingState struct {
	pathSegments Path   // Current decoding path as segments. Always available.
	path         []byte // Current decoding path, for saving as a reference outside the struct. Never modified. Computed as necessary.
}

func NewDecodingState() *DecodingState {
	return &DecodingState{path: make([]byte, 0)}
}

func (d *DecodingState) Path() []byte {
	if len(d.path) == 0 {
		d.path = []byte(d.pathSegments.String())
	}

	return d.path
}

func (d *DecodingState) PathSegments() Path {
	return d.pathSegments
}

func (d *DecodingState) PushIndex(i int) {
	d.pathSegments = append(d.pathSegments, NewPathArrayElementRef(i))

	d.path = d.path[:0]
}

func (d *DecodingState) PushField(k string) {
	d.pathSegments = append(d.pathSegments, NewPathObjectPropertyRef(k, true))
	d.path = d.path[:0]
}

func (d *DecodingState) Pop() {
	d.pathSegments = d.pathSegments[0 : len(d.pathSegments)-1]
	d.path = d.path[:0]
}
