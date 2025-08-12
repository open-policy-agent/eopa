<!-- markdownlint-disable MD041 -->
| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.scheme` | String | Yes | | Determines the type of auth credentials to use with Neo4J. Must be one of `none`, `basic`, `kerberos`, or `bearer` |
| `auth.principal` | String | No | | Stores the username when `auth.scheme` is `basic`. |
| `auth.credentials` | String | No |  | Stores the password when `auth.scheme` is `basic`, the token when `auth.scheme` is `bearer`, and the ticket when `auth.scheme` is `kerberos`. |
| `auth.realm` | String | No | | Stores the (optional) realm when `auth.scheme` is `basic`. |
