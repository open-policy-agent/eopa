package config
import rego.v1

discovery := {
  "default_decision": "acmecorp/httpauthz/allow"
} if data.foo.bar == "baz"
