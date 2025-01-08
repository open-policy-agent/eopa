package test

# METADATA
# entrypoint: true
result if foo[input.yay]

foo := data.x.foo

# uses EOPA-only builtin
bar := sql.send({})
