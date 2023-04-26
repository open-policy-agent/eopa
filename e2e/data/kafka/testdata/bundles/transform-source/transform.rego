package transform
import future.keywords
transform contains {"op": "add", "path": key, "value": val} if {
	payload := json.unmarshal(base64.decode(input.value))
	key := base64.decode(input.key)
	val := {
		"value": payload,
		"headers": input.headers,
	}
}
