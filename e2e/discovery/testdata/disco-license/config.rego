package config
import rego.v1

discovery := {
  "default_decision": "acmecorp/httpauthz/allow",
  "eopa": {
    "license": "VALID",
  }
} if data.foo.bar == "baz"
