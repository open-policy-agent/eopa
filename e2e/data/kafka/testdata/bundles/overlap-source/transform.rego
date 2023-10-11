package transform
import future.keywords
transform[key] := val if {
	some msg in input.incoming
	payload := json.unmarshal(base64.decode(msg.value))
	key := base64.decode(msg.key)
	val := {
		"value": payload,
		"headers": msg.headers,
	}
}

# merge with old
transform[key] := val if {
	some key, val in input.previous
	every msg in input.incoming {
		key != base64.decode(msg.key) # incoming batch takes precedence
	}
}
