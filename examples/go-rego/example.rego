package example

import rego.v1

default allow := false                              # unless otherwise defined, allow is false

allow := true if {                                  # allow is true if...
    count(violation) == 0                           # there are zero violations.
}

violation[server.id] if {                           # a server is in the violation set if...
    some server in public_server                    # it exists in the 'public_server' set and...
    "http" in server.protocols                      # it contains the insecure "http" protocol.
}

violation[server.id] if {                           # a server is in the violation set if...
    some server in input.servers                    # it exists in the input.servers collection and...
    "telnet" in server.protocols                    # it contains the "telnet" protocol.
}

public_server[server] if {                          # a server exists in the public_server set if...
    some i, j
    some server in input.servers                    # it exists in the input.servers collection and...
    input.ports[i].id in server.ports               # it references a port in the input.ports collection and... # regal ignore:line-length
    input.ports[i].network == input.networks[j].id  # the port references a network in the input.networks collection and... # regal ignore:line-length
    input.networks[j].public                        # the network is public.
}