---
sidebar_position: 3
sidebar_label: Okta
title: Okta Datasource Configuration | EOPA
---

import PluginDataKey from './_plugin-data-key.mdx'


# Okta Datasource Configuration

The EOPA Okta datasource plugin pulls in data from Okta's APIs, making
it possible to have all your users and groups data managed via Okta available
for policy evaluations in EOPA.


## Example Configuration

The Okta integration is provided via the `data` plugin, and needs to be enabled
in the EOPA configuration via a plugin with `type: okta`


### Minimal

```yaml
# eopa-conf.yaml
plugins:
  data:
    okta.corp:
      type: okta
      url: https://YOURTENANT.okta.com
      client_id: OKTA_CLIENT_ID
      key_id: OKTA_CLIENT_KEY_ID
      private_key: |
        -----BEGIN PRIVATE KEY-----
        ...
        -----END PRIVATE KEY-----
      users: true
      groups: true
      roles: true
      apps: true
      polling_interval: 10m  # default: 5m, minimum 10s
```

With this minimal configuration, EOPA will pull in all data about users, groups,
roles and apps from the Okta API every 30 seconds.

<!-- markdownlint-disable MD044 -->
:::info About the data key
<PluginDataKey configKey="okta.corp" subtree="/data/okta/corp" />
:::
<!-- markdownlint-enable MD044 -->

:::warning
The Okta API has fairly aggressive rate limiting. A single EOPA instance polling
all of the data out of an Okta tenant may exhaust a substantial portion of the Okta
account's rolling rate limit.

Users should be aware that deploying many EOPA instances all connected to Okta may
result in the Okta account's rate limit being exceeded, and users with this use case
may wish to poll Okta from a central location and distribute the resulting data internally.
:::


### Requirements

The plugin requires an Okta application set up using "API Services" as a sign-in method. More information about setting up a service app with Okta can be found
in the [Okta documentation](https://developer.okta.com/docs/guides/implement-oauth-for-okta-serviceapp/main/).


## Configuration

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `url` | `string` | Yes |  | Address to connect to Okta. |
| `client_id` | `string` | Yes |  | The `Client ID` of the connected Okta application. |
| `key_id` | `string` | Yes |  | The `KID` of the public key for the connected Okta application.|
| `private_key` | `string` | Yes |  | The private key (in PEM format) for the connected Okta application. |
| `users` | `bool` | No | false | Retrieve users from Okta (see note below) |
| `groups` | `bool` | No | false | Retrieve groups from Okta (see note below) |
| `roles` | `bool` | No | false | Retrieve roles from Okta (see note below) |
| `apps` | `bool` | No | false | Retrieve apps from Okta (see note below) |

:::note On Scopes
The data that should be retrieved depends on scopes ("Okta API Scopes") granted to the client. The following scopes are needed to retrieve the corresponding item:

<!-- markdownlint-disable MD044 -->
item | scopes
---|---
users | okta.users.read
groups | okta.groups.read
roles | okta.roles.read
apps | okta.apps.read
:::
<!-- markdownlint-enable MD044 -->

## Example Call

Using the Okta data plugin in EOPA with an Okta developer account,
and all the client scopes listed above granted, this is an example of the shape
and amount of data we gather:

```json
# terminal-command
curl "http://127.0.0.1:8181/v1/data/okta/corp?pretty"
{
  "result": {
    "apps": [
        {
        "_links": {
          "accessPolicy": {"href": "https://dev-66206905.okta.com/api/v1/policies/rst9nj67mtzf56rJM5d7"},
          "appLinks": [
            {
              "href": "https://dev-66206905.okta.com/home/saasure/0oa9nhiwcj77Tysic5d7/2",
              "name": "admin",
              "type": "text/html"
            }
          ],
          "deactivate": { "href": "https://dev-66206905.okta.com/api/v1/apps/0oa9nhiwcj77Tysic5d7/lifecycle/deactivate"},
          "groups": {"href": "https://dev-66206905.okta.com/api/v1/apps/0oa9nhiwcj77Tysic5d7/groups"},
          "logo": [
            {
              "href": "https://ok12static.oktacdn.com/assets/img/logos/okta_admin_app.da3325676d57eaf566cb786dd0c7a819.png",
              "name": "medium",
              "type": "image/png"
            }
          ],
          "policies": {
            "hints": {"allow": ["PUT"]},
            "href": "https://dev-66206905.okta.com/api/v1/apps/0oa9nhiwcj77Tysic5d7/policies"
          },
          "profileEnrollment": {"href": "https://dev-66206905.okta.com/api/v1/policies/rst9nj67o77XYDmlA5d7"},
          "uploadLogo": {
            "hints": {"allow": ["POST"]},
            "href": "https://dev-66206905.okta.com/api/v1/apps/0oa9nhiwcj77Tysic5d7/logo"
          },
          "users": {"href": "https://dev-66206905.okta.com/api/v1/apps/0oa9nhiwcj77Tysic5d7/users"}
        },
        "accessibility": {"selfService": false},
        "created": "2023-05-22T09:10:25Z",
        "credentials": {
          "signing": {"kid": "qpvZFtTdCKiB0VfYu_prtvQfqVZIPcHIxWgEMUXuMJc"},
          "userNameTemplate": {
            "template": "${source.login}",
            "type": "BUILT_IN"
          }
        },
        "features": [],
        "id": "0oa9nhiwcj77Tysic5d7",
        "label": "Okta Admin Console",
        "lastUpdated": "2023-05-22T09:10:25Z",
        "name": "saasure",
        "settings": {
          "app": {},
          "notifications": {"vpn": { "network": { "connection": "DISABLED"}}}
        },
        "signOnMode": "OPENID_CONNECT",
        "status": "ACTIVE",
        "visibility": {
          "appLinks": {"admin": true},
          "autoSubmitToolbar": false,
          "hide": {
            "iOS": false,
            "web": false
          }
        }
      }
    ],
    "group-members": {
      "00g9nhiwcxttZ8oTG5d7": [
        {
          "_links": "self": {"href": "https://dev-66206905.okta.com/api/v1/users/00u9nhiwi1YaBEgA75d7"}},
          "activated": null,
          "created": "2023-05-22T09:10:29Z",
          "credentials": {
            "password": {},
            "provider": {
              "name": "OKTA",
              "type": "OKTA"
            }
          },
          "id": "00u9nhiwi1YaBEgA75d7",
          "lastLogin": "2023-05-23T07:57:02Z",
          "lastUpdated": "2023-05-22T11:28:26Z",
          "passwordChanged": "2023-05-22T11:28:26Z",
          "profile": {
            "email": "alice@example.com",
            "firstName": "Alice",
            "lastName": "Schmidt",
            "login": "alice@example.com",
            "mobilePhone": null,
            "secondEmail": null
          },
          "status": "RECOVERY",
          "statusChanged": "2023-05-22T11:28:11Z",
          "type": {"id": "oty9nhiwd68ebBiXn5d7"}
        }
      ],
      "00g9nhiwghzhcqCiL5d7": []
    },
    "groups": [
      {
        "_links": {
          "apps": {"href": "https://dev-66206905.okta.com/api/v1/groups/00g9nhiwcxttZ8oTG5d7/apps"},
          "logo": [
            {
              "href": "https://ok12static.oktacdn.com/assets/img/logos/groups/odyssey/okta-medium.1a5ebe44c4244fb796c235d86b47e3bb.png",
              "name": "medium",
              "type": "image/png"
            },
            {
              "href": "https://ok12static.oktacdn.com/assets/img/logos/groups/odyssey/okta-large.d9cfbd8a00a4feac1aa5612ba02e99c0.png",
              "name": "large",
              "type": "image/png"
            }
          ],
          "users": {"href": "https://dev-66206905.okta.com/api/v1/groups/00g9nhiwcxttZ8oTG5d7/users"}
        },
        "created": "2023-05-22T09:10:25Z",
        "id": "00g9nhiwcxttZ8oTG5d7",
        "lastMembershipUpdated": "2023-05-22T09:10:29Z",
        "lastUpdated": "2023-05-22T09:10:25Z",
        "objectClass": [ "okta:user_group"],
        "profile": {
          "description": "All users in your organization",
          "name": "Everyone"
        },
        "type": "BUILT_IN"
      },
      {
        "_links": {
          "apps": {"href": "https://dev-66206905.okta.com/api/v1/groups/00g9nhiwghzhcqCiL5d7/apps"},
          "logo": [
            {
              "href": "https://ok12static.oktacdn.com/assets/img/logos/groups/odyssey/okta-medium.1a5ebe44c4244fb796c235d86b47e3bb.png",
              "name": "medium",
              "type": "image/png"
            },
            {
              "href": "https://ok12static.oktacdn.com/assets/img/logos/groups/odyssey/okta-large.d9cfbd8a00a4feac1aa5612ba02e99c0.png",
              "name": "large",
              "type": "image/png"
            }
          ],
          "users": {"href": "https://dev-66206905.okta.com/api/v1/groups/00g9nhiwghzhcqCiL5d7/users"}
        },
        "created": "2023-05-22T09:10:28Z",
        "id": "00g9nhiwghzhcqCiL5d7",
        "lastMembershipUpdated": "2023-05-22T09:10:28Z",
        "lastUpdated": "2023-05-22T09:10:28Z",
        "objectClass": [ "okta:user_group"],
        "profile": {
          "description": "Okta manages this group, which contains all administrators in your organization.",
          "name": "Okta Administrators"
        },
        "type": "BUILT_IN"
      }
    ],
    "roles": [
      {
        "_links": {
          "permissions": {"href": "https://dev-66206905-admin.okta.com/api/v1/iam/roles/cr09o4klifKJs6rkO5d7/permissions"},
          "self": {"href": "https://dev-66206905-admin.okta.com/api/v1/iam/roles/cr09o4klifKJs6rkO5d7"}
        },
        "created": "2023-05-23T10:29:33Z",
        "description": "foobearers",
        "id": "cr09o4klifKJs6rkO5d7",
        "label": "foobearers",
        "lastUpdated": "2023-05-23T10:29:33Z",
        "permissions": null
      }
     ],
    "users": [
      {
        "_links": {"self": {"href": "https://dev-66206905.okta.com/api/v1/users/00u9nhiwi1YaBEgA75d7"}},
        "activated": null,
        "created": "2023-05-22T09:10:29Z",
        "credentials": {
          "password": {},
          "provider": {
            "name": "OKTA",
            "type": "OKTA"
          }
        },
        "id": "00u9nhiwi1YaBEgA75d7",
        "lastLogin": "2023-05-23T07:57:02Z",
        "lastUpdated": "2023-05-22T11:28:26Z",
        "passwordChanged": "2023-05-22T11:28:26Z",
        "profile": {
          "email": "alice@example.com",
          "firstName": "Alice",
          "lastName": "Schmidt",
          "login": "alice@example.com",
          "mobilePhone": null,
          "secondEmail": null
        },
        "status": "RECOVERY",
        "statusChanged": "2023-05-22T11:28:11Z",
        "type": {"id": "oty9nhiwd68ebBiXn5d7"}
      }
    ]
  }
}
```

:::note
Data under `apps` is unwieldy, so only the first app has been included in the example above.
:::


## Data Transformations

The `rego_transform` attribute specifies the path to a rule used to transform data pulled from Okta into a different format for storage in EOPA.

`rego_transform` policies take incoming messages as JSON via `input.incoming` and returns the transformed JSON.


### Example

Starting with the EOPA configuration above and the example data above

Our `data.e2e.transform` policy is:

```rego
package e2e

import rego.v1

transform.users[id] := d if {
	some entry in input.incoming.users
	id := entry.id
	d := object.filter(entry.profile, {"firstName", "email"})
}
```

Then the data retrieved by the Okta plugin would be transformed by the above into:

```json
# terminal-command
curl "${EOPA_URL}/v1/data/okta/corp?pretty"
{
  "result": {
    "users": {
      "00u9nhiwi1YaBEgA75d7": {
        "email": "alice@example.com",
        "firstName": "Alice"
      }
    }
  }
}
```
