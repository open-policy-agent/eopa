package transform

import rego.v1

_payload(msg) := json.unmarshal(base64.decode(msg.value))

# _kind() of a message determines where on the resulting data tree its
# values are to be written. This is useful when you don't want to use the
# incoming topics as-is.
_kind(msg) := "groups" if msg.topic == "group-topic"

else := msg.topic

_values("users", payload) := object.filter(payload, ["name", "roles"]) # for users, pluck "name" and "roles"

_values("groups", payload) := object.remove(payload, ["id", "type"]) # for groups, take everything BUT "type"

_values(kind, payload) := payload if not kind in {"users", "groups"} # for other kinds, keep message payload as-is

# this collects all kinds of the messages in a batch, to determine which aren't updated by the batch
batches contains _kind(msg) if some msg in input.incoming

# incoming messages are parsed and stored under their ID payload field
transform[kind][key] := _values(kind, payload) if {
	some kind, payloads in incoming_latest
	some _, payload in payloads
	key := payload.id
	not is_delete(payload)
}

# no "type" in the payload means "not delete" => undefined
is_delete(payload) if payload.type == "delete"

# if no new data came in for a certain message, we'll retain the data
# stored previously
transform[kind][key] := val if {
	# we cannot read existing kinds from batches, as we'd lose old data
	# that has no updates in the incoming batch
	some kind, existing in input.previous
	some key, val in existing
	no_news_for_key(kind, key)
}

no_news_for_key(kind, _) if not kind in batches

no_news_for_key(kind, key) if not incoming_latest[kind][key]

# input.incoming is an array, iteration order naturally given
incoming[_kind(msg)][key][idx] := payload if {
	some idx, msg in input.incoming
	payload := _payload(msg)
	key := payload.id
}

incoming_latest[kind][key] := payload if {
	some kind, xs in incoming
	some key, batch in xs

	# batch is an object! iteration order is unspecified -- but it happens to be sorted
	arr := [i | some i, _ in batch]
	last := arr[count(arr) - 1]
	payload := batch[last]
}
