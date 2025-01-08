package play

default hello := false

hello if {
	m := input.message
	m == "world"

	msg := input.data.msg
	mm = lower(msg)
	mm == "u"

	n1 := input.A.B.C
	n2 := lower(n1)
	n2 == "abc"

	m1 := input.B.B.C.D.E
	m2 := lower(m1)
	m2 == "e"
}

deny contains msg if {
	image := input.request1.object1.spec1.containers1.image1
	not startswith(image, "xxxxxci.dockerhub")
	msg := sprintf("image is not from trusted registry: %v", [image])
}

deny contains msg if {
	image := input.request2.object2.spec2.containers2.image2
	not startswith(image, "xxxxxci.dockerhub")
	msg := sprintf("image is not from trusted registry: %v", [image])
}

deny contains msg if {
	image := input.request3.object3.spec3.containers3.image3
	not startswith(image, "xxxxxci.dockerhub")
	msg := sprintf("image is not from trusted registry: %v", [image])
}

deny contains msg if {
	x := 1
	y := 2
	x < y
	image := input.request4.object4.spec4.containers4.image4
	not startswith(image, "xxxxxci.dockerhub")
	msg := sprintf("image is not from trusted registry: %v", [image])
}

deny contains msg if {
	a := input.servers[0].protocols[_]
	b := input.servers[0].ports[_]
	c := input.servers[_].id
	d := input.servers[_].protocols
	e := input.servers[_].ports
	f := input.networks[_].id
	g := input.networks[_].public
	h := input.ports[_].id
	i := input.ports[_].network
	not startswith(a, "xxxxxci.dockerhub")
	msg := sprintf(": %v", [i])
}

deny contains msg if {
	a := input.sites[_].region
	b := input.sites[_].name
	c := input.sites[_].servers
	d := input.sites[_].servers[_].name
	e := input.sites[_].servers[_].hostname
	f := input.apps[_].name
	g := input.apps[_].servers[_]
	h := input.containers[_].image
	i := input.containers[_].ipaddress
	j := input.containers[_].name
	r1 := combine(a, b)
	r2 := combine(b, c)
	r3 := combine(c, d)
	r4 := combine(d, e)
	r5 := combine(e, f)
	image := input.request5.object5.spec5.containers5.image5
	not startswith(image, "xxxxxci.dockerhub")
	msg := sprintf("image is not from trusted registry: %v", [image])
}

combine(i, j) := result if {
	result = [i, j]
}

deny contains {"msg": msg} if {
	hosts_set := valid_ingress_hosts
	len_set := count(hosts_set)
	len_set > 0

	#// converts set to array/list
	array_list := [x | x := hosts_set[_]]
	isarray := is_array(array_list)

	host1 := "host1.1.1.1.1"
	host2 := "host2.2.2.2.2"
	host3 := "host3.3.3.3.3"
	host4 := "host4.4.4.4.4"
	host5 := "host5.5.5.5.5"
	all_known_hosts := [host1, host2, host3, host4, host5]

	#// is {host1-5} in set: {hosts_set}
	hosts_set[host1]
	hosts_set[host2]
	hosts_set[host3]
	hosts_set[host4]
	hosts_set[host5]

	#// checks if "host5" is in array/list
	array_list[i] == host5
	array_list[_] == all_known_hosts[k]

	fqdn_matches_any(all_known_hosts, hosts_set)
	image := input.request6.object6.spec6.containers6.image6
	not startswith(image, "xxxxxci.dockerhub")

	#msg := sprintf("image is not from trusted registry: %v", [image])
	#msg := sprintf("hosts_set: %v", [hosts_set])
	#msg := sprintf("array_list: %v", [array_list])
	#msg := sprintf("isarray: %v", [isarray])
	msg := sprintf("array_list[j]: %v", [array_list[j]])
}

#//=================================================================================================
deny contains {"msg": msg} if {
	input.request7.kind.kind == "Pod"
	input.request8.operation == "CREATE"
	container := input_container[_]
	not container.livenessProbe
	msg := sprintf("container is missing livenessProbe: %v", [container])
}

deny contains {"msg": msg} if {
	input.request7.kind.kind = "Pod"
	input.request8.operation = "CREATE"
	container := input_container[_]
	not container.readinessProbe
	msg := sprintf("container is missing readinessProbe: %v", [container])
}

input_container contains container if {
	container := input.request9.object.spec.containers[_]
}

#//=================================================================================================
#// violation
#//=================================================================================================
violation contains {"msg": msg, "details": {"missing_labels": missing}} if {
	provided := {label | label := input.review.object.metadata.labels[_]}
	required := {label | label := input.parameters.labels[_]}
	missing := required - provided
	count(missing) > 0
	msg := sprintf("you must provide labels: %v", [missing])
}

#//=================================================================================================
#// valid_ingress_hosts
#//=================================================================================================
valid_ingress_hosts := {hosts_set |
	whitelist := "*.host1.1.1.1.1,*.host2.2.2.2.2,*.host3.3.3.3.3,*.host4.4.4.4.4,*.host5.5.5.5.5"
	hosts := split(whitelist, ",")
	hostsset := trim(hosts[_], "*.")
	hosts_set := hostsset
}

#//=================================================================================================
#// fqdn_matches_any
#//=================================================================================================
fqdn_matches_any(all_known_hosts, hosts_set) if {
	some i

	#match := hosts_set[all_known_hosts[i]]
	hosts_set[all_known_hosts[i]] == all_known_hosts[i]
}
