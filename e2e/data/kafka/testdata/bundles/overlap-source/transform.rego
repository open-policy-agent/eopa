package transform
import future.keywords
transform[key] := val if {
	some msg in input
	payload := json.unmarshal(base64.decode(msg.value))
	key := base64.decode(msg.key)
	val := {
		"value": payload,
		"headers": msg.headers,
	}
}

# merge with old
transform[key] := val if {
	some key, val in data.kafka.messages
	every msg in input {
		key != base64.decode(msg.key) # incoming batch takes precedence
	}
}