<!-- markdownlint-disable MD041 -->
| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.auth_mechanism` | String | No |  | The mechanism to use for authentication. Supported values include `SCRAM-SHA-256`, `SCRAM-SHA-1`, `MONGODB-CR`, `PLAIN`, `GSSAPI`, `MONGODB-X509`, and `MONGODB-AWS`. [More details](https://www.mongodb.com/docs/manual/core/authentication-mechanisms/). |
| `auth.auth_mechanism_properties` | Object | No |  | Additional configuration options for certain mechanisms. [More Details](https://www.mongodb.com/docs/manual/reference/connection-string/#mongodb-urioption-urioption.authMechanismProperties) |
| `auth.auth_source` | String | No |  | The name of the database to use for authentication. [More Details](https://www.mongodb.com/docs/manual/reference/connection-string/#mongodb-urioption-urioption.authSource). |
| `auth.username` | String | No |  | The username for authentication. |
| `auth.password` | String | No |  | The password for authentication. |
| `auth.password_set` | Bool | No |  | For GSSAPI, this must be true if a password is specified, even if the password is the empty string, and false if no password is specified, indicating that the password should be taken from the context of the running process. For other mechanisms, this field is ignored. |

See links below to the MongoDB docs for more information on some of the options:

- [auth_mechanism](https://www.mongodb.com/docs/manual/core/authentication-mechanisms/)
- [auth_mechanism_properties](https://www.mongodb.com/docs/manual/reference/connection-string/#mongodb-urioption-urioption.authMechanismProperties)
- [auth_source](https://www.mongodb.com/docs/manual/reference/connection-string/#mongodb-urioption-urioption.authSource)
