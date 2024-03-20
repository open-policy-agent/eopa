package test

import rego.v1

allow if input.action in data.roles[data.users[input.user].role]
