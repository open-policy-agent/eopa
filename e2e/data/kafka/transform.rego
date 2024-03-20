package e2e

import rego.v1

_payload(msg) := json.unmarshal(base64.decode(msg.value))
_key(msg) := json.unmarshal(base64.decode(msg.key))

batch_ids contains id if some id, _ in incoming_latest

transform.users[payload.id] := val if {
	some msg in incoming_latest
	payload := _payload(msg)
	payload.type != "delete"
	val	:= object.filter(payload, ["name"])
}

transform.users[key] := val if {
	some key, val in input.previous
	not key in batch_ids
}

# counting all processed messages lets us assert that no single batch was lost
# due to some error, like "object insert conflict" or "instruction limit exceeded"
transform.count := object.get(input.previous, "count", 0) + count(input.incoming)

incoming[id][key] := msg if {
    some msg in input.incoming
    key := _key(msg)
    id := _payload(msg).id
}

incoming_latest[id] := msg if {
    some id, batch in incoming
    ks := [ k | some k, _ in batch ]
    latest := max(ks)
    msg := batch[latest]
}
