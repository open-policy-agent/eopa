package convert

converted := ucast.as_sql(conds(input.compile.result), input.dialect, input.replacements)

conds(pe) := res if {
	not pe.support # "support modules" are not supported right now
	res := or_({query_to_condition(q) | some q in pe.queries})
}

query_to_condition(q) := and_({expr_to_condition(e) | some e in q})

expr_to_condition(e) := op_(op(e), field(e), value(e))

op(e) := r if {
	e.terms[0].type == "ref"
	e.terms[0].value[0].type == "var"
	o := e.terms[0].value[0].value
	r := is_valid(o)
}

is_valid(o) := u if {
	o in {"eq", "lt", "gt", "neq", "startswith", "endswith", "contains"}
	u := _replace(o)
}

_replace("neq") := "ne"

_replace(x) := x if x != "neq"

field(e) := f if {
	# find the operand with 'input.*'
	some t in array.slice(e.terms, 1, 3)
	is_input_ref(t)
	f := concat(".", [t.value[1].value, t.value[2].value])
}

value(e) := v if {
	# find the operand without 'input.*'
	some t in array.slice(e.terms, 1, 3)
	not is_input_ref(t)
	v := value_from_term(t)
}

value_from_term(t) := t.value if t.type != "null"

else := null

is_input_ref(t) if {
	t.type == "ref"
	t.value[0].value == "input"
}

# conditions helper functions
eq_(field, value) := op_("eq", field, value)

lt_(f, v) := op_("lt", f, v)

op_(o, f, v) := {"type": "field", "operator": o, "field": f, "value": v}

and_(values) := compound("and", values)

or_(values) := compound("or", values)

compound(op, values) := {"type": "compound", "operator": op, "value": values}
