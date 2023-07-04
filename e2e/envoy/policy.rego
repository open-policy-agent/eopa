package envoy.authz
import future.keywords.if

default allow := false

allow if input.parsed_path = ["yages.Echo", "Ping"] # Ping is OK

allow if {
  input.parsed_path = ["yages.Echo", "Reverse"]
  input.parsed_body = {
    "text": "Maddaddam"
  }
}
