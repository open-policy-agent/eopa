package e2e

import future.keywords.contains
import future.keywords.every
import future.keywords.if
import future.keywords.in

_payload(msg) := json.unmarshal(base64.decode(msg.value))

batch_ids contains _payload(msg).id if some msg in input.incoming

transform[payload.id] := val if {
	some msg in input.incoming
	msg.topic == "users"

	payload := _payload(msg)
	val	:= object.filter(payload, ["name", "roles"])
}

transform[key] := val {
	some key, val in input.previous
	not key in batch_ids
}
