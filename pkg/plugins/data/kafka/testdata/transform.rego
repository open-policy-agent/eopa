package e2e

import future.keywords.contains
import future.keywords.if

transform contains {"op": "add", "path": payload.id, "value": val} if {
    input.topic == "styra-topic"
 
    payload := json.unmarshal(base64.decode(input.value))
    val := object.filter(payload, ["name", "roles"])
}
