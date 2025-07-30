# regal ingore:rule-name-repeats-package
package generate

import rego.v1

# policy

default allow := false

allow if {
	some role in data.users[input.user]
	some permission in data.roles[role]
	permission.action == input.action
	permission.resource == input.resource
}

# generation

range_upto_random_n(keys, n) := numbers.range(1, rand.intn(sprintf("key-%s", [concat("-", keys)]), n))

user_ids := [u | u := sprintf("user%d", [numbers.range(1, input.users)[_]])]

role_ids := [u | u := sprintf("role%d", [numbers.range(1, input.roles)[_]])]

action_ids := [u | u := sprintf("action%d", [numbers.range(1, input.actions)[_]])]

resource_ids := [u | u := sprintf("resource%d", [numbers.range(1, input.resources)[_]])]

users[user_id] := roles if {
	some user_id in user_ids

	roles := {r |
		some i in range_upto_random_n(["user", user_id], input.max_roles_per_user)
		r := role_ids[rand.intn(sprintf("%s%d", [user_id, i]), count(role_ids))] # regal ignore:comprehension-term-assignment
	}
}

roles[role_id] := permission if {
	some role_id in role_ids

	permission := {p |
		some i in range_upto_random_n(["role", role_id], input.max_capabilities_per_role)
		action_id := action_ids[rand.intn(sprintf("%s%d-action", [role_id, i]), count(action_ids))]
		resource_id := resource_ids[rand.intn(sprintf("%s%d-resource", [role_id, i]), count(resource_ids))]

		p := {
			"action": action_id,
			"resource": resource_id,
		}
	}
}

queries := [q |
	some i in numbers.range(1, input.queries)
	user_id := user_ids[rand.intn(sprintf("%d-user", [i]), count(user_ids))]
	action_id := action_ids[rand.intn(sprintf("%d-action", [i]), count(action_ids))]
	resource_id := resource_ids[rand.intn(sprintf("%d-resource", [i]), count(resource_ids))]

	# regal ignore:with-outside-test-context
	allowed := allow with input as {"user": user_id, "action": action_id, "resource": resource_id}
		with data.users as users
		with data.roles as roles

	q := {
		"input": {
			"user": user_id,
			"action": action_id,
			"resource": resource_id,
		},
		"expected": allowed,
	}
]
