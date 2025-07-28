package authz

import future.keywords.if
import future.keywords.in

default allow := false

# admin can do anything
allow if input.username == "admin"

# users can update their own records
allow if {
	path := split(input.path, "/")
	path[1] == "data"
	path[2] == input.username
	input.method == "POST"
}

# reading records for authenticated users only
allow if {
	path := split(input.path, "/")
	path[1] == "data"
	input.method == "GET"
	input.username != "nobody"
}
