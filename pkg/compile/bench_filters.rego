# METADATA
# scope: package
# custom:
#   unknowns:
#     - input.tickets
#     - input.users
package filters

tenancy if input.tickets.tenant == input.tenant.id # tenancy check

include if {
	tenancy
	resolver_include
}

include if {
	tenancy
	not user_is_resolver(input.user, input.tenant.name)
}

resolver_include if {
	user_is_resolver(input.user, input.tenant.name)

	# ticket is assigned to user
	input.users.name == input.user
}

resolver_include if {
	user_is_resolver(input.user, input.tenant.name)

	# ticket is unassigned and unresolved
	input.tickets.assignee == null
	input.tickets.resolved == false
}

user_is_resolver(user, tenant) if "resolver" in data.roles[tenant][user]

_use_metadata := rego.metadata.rule()
