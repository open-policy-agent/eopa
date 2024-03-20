package test
import rego.v1

# METADATA
# entrypoint: true
result if foo[input.yay]

foo := data.x.foo
