package authz

import rego.v1

default allow := false

# users can update their own records
allow if {
	path := split(input.path, "/")
	path[1] == "data"
	path[2] == input.username
	input.method == "POST"
}

# users can read all records
allow if {
	path := split(input.path, "/")
	path[1] == "data"
	input.method == "GET"
}