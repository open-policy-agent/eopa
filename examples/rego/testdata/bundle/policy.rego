package test

import future.keywords.if
import future.keywords.in

allow if input.action in data.roles[data.users[input.user].role]