---
sidebar_position: 4
sidebar_label: LDAP
title: LDAP Configuration | EOPA
---

# LDAP

EOPA's support for pulling in data from any LDAP server makes it possible to
have all your user and groups data managed in directory services available for policy
evaluations in EOPA.


## Example Configuration

The LDAP integration is provided via the `data` plugin, and needs to be enabled in EOPA's configuration.


### Minimal

```yaml
# eopa-conf.yaml
plugins:
  data:
    ldap.users:
      type: ldap
      urls:
      - ldap://internal.ldapd:1389
      base_dn: ou=users,dc=example,dc=org
```

With this minimal configuration, EOPA will pull in all attributes of all
objects found under the base DN  `ou=users,dc=example,dc=org` every 30 seconds.

All of this, and various other search- and TLS-related settings, can be configured
using an advanced configuration:


### Advanced

```yaml
# eopa-conf-advanced.yaml
plugins:
  data:
    ldap.users:
      type: ldap
      urls:
      - ldap://internal.ldapd:1389
      base_dn: ou=users,dc=example,dc=org
      filter: (objectClass=inetOrgPerson)
      attributes:            # only pull in certain attributes
      - cn
      - sn
      - uid
      scope: base-object     # one of "base-object", "single-level", "whole-subtree" (default)
      deref: always          # one of "never" (default), "searching", "finding", "always"
      polling_interval: 10m  # default: 30s, minimum 10s
 
      username: alice              # bind username
      password: wordpass           # bind password

      tls_skip_verification: true
      tls_client_cert: cert.pem
      tls_ca_cert: ca.pem
      tls_client_private_key: key.pem # key, file path or PEM contents

      rego_transform: data.e2e.transform
```

With a config like this, EOPA will periodically perform an LDAP search
according to the configured parameters, and provide the retrieved data in the
configured subtree, e.g. `data.ldap.users`.

:::note
Since LDAP objects can have multiple values for each key, the pulled-in data
contains _array values_ for all attributes.

See the example below for details.
:::


## Example Call

If your LDAP service contains two users, `cn=user01,ou=users,dc=example,dc=org` and
`cn=user02,ou=users,dc=example,dc=org`, as returned by an `ldapsearch` query:

```yaml
# terminal-command
ldapsearch -x -h 127.0.0.1:1389 -b 'ou=users,dc=example,dc=org' '(objectClass=inetOrgPerson)' sn cn uid
# extended LDIF
#
# LDAPv3
# base <ou=users,dc=example,dc=org> with scope subtree
# filter: (objectClass=inetOrgPerson)
# requesting: sn cn 
#

# user01, users, example.org
dn: cn=user01,ou=users,dc=example,dc=org
cn: User1
cn: user01
uid: user01
sn: Bar1

# user02, users, example.org
dn: cn=user02,ou=users,dc=example,dc=org
cn: User2
cn: user02
uid: user02
sn: Bar2

# search result
search: 2
result: 0 Success

# numResponses: 3
# numEntries: 2
```

and you've configured your EOPA instance as above, you will
be able to see the entities appear in your `data.ldap.users` tree:


```json
# terminal-command
curl "http://127.0.0.1:8181/v1/data/ldap/users?pretty"
{
  "result": [
    {
      "cn": [
        "User1",
        "user01"
      ],
      "dn": {
        "_raw": "cn=user01,ou=users,dc=example,dc=org",
        "cn": [
          "user01"
        ],
        "dc": [
          "example",
          "org"
        ],
        "ou": [
          "users"
        ]
      },
      "sn": [
        "Bar1"
      ],
      "uid": [
        "user01"
      ]
    },
    {
      "cn": [
        "User2",
        "user02"
      ],
      "dn": {
        "_raw": "cn=user02,ou=users,dc=example,dc=org",
        "cn": [
          "user02"
        ],
        "dc": [
          "example",
          "org"
        ],
        "ou": [
          "users"
        ]
      },
      "sn": [
        "Bar2"
      ],
      "uid": [
        "user02"
      ]
    }
  ]
}
```

As mentioned above, all values are arrays. This needs to be taken into account when writing
policies against that data.

For example, to match `input.user` against this data retrieved from LDAP, you'd write
```rego
package main

import rego.v1

allow if matches_user(input.user)

matches_user(user) if {
	some entry in data.ldap.users
	user in entry.uid # NOT entry.uid == user
}
```

:::note
The **key** below `data` in the configuration (`git.users` in the example) can be anything you want,
and determines where the retrieved document will be found in EOPA's `data` hierarchy.
:::


## Data Transformations

The `rego_transform` attribute specifies the path to a rule used to transform data pulled from LDAP into a different format for storage in EOPA.

`rego_transform` policies take incoming messages as JSON via `input.incoming` and returns the transformed JSON.


### Example

Starting with the EOPA configuration above, but using [LLDAP](https://github.com/lldap/lldap/) with users `alice (Alice Abramson)` and `bob (Bob Branzino)`, and groups `admin` (with `alice` and `bob`), and `superadmin` (with `alice`).

Our `data.e2e.transform` policy is:

```rego
package e2e

import rego.v1

transform.users[id] := y if {
	some entry in input.incoming
	"inetOrgPerson" in entry.objectclass
	id := entry.uid[0]
	y := {"name": entry.cn[0]}
}

transform.groups[id] := members if {
	some entry in input.incoming
	"groupOfUniqueNames" in entry.objectclass
	id := entry.cn[0]
	members := member_ids(entry.uniquemember)
	not startswith(id, "lldap_")
}

member_ids(uids) := {id |
	some entry in input.incoming
	"inetOrgPerson" in entry.objectclass
	entry.dn._raw in uids
	id := entry.uid[0]
}
```

Then the data retrieved by the LDAP plugin would be transformed by the above into:

```json
# terminal-command
curl "${ENTERPRISE_OPA_URL}/v1/data/ldap/entities?pretty"
{
  "result": {
    "groups": {
      "admin": [
        "alice",
        "bob"
      ],
      "superadmin": [
        "alice"
      ]
    },
    "users": {
      "alice": {
        "name": "Alice Abramson"
      },
      "bob": {
        "name": "Bob Branzino"
      }
    }
  }
}
```
