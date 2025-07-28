---
sidebar_position: 1
sidebar_label: From Hashicorp Vault
title: Using Secrets from HashiCorp Vault | Enterprise OPA
---

# Using HashiCorp Vault as an External Key Manager

Enterprise OPA supports integrating with [HashiCorp Vault](https://www.vaultproject.io/use-cases/secrets-management) as a third-party External Key Manager.

The Enterprise OPA Vault integration can be used to:
- Retrieve the Enterprise OPA [License key](#license-keys) from a Vault secret
- Override the configuration for an Enterprise OPA [service or key configuration](#services-and-keys-overrides)
- Override the configuration of [`http.send`](#httpsend-overrides)


## Configuration

1. [Connecting Vault to Enterprise OPA](#connecting-vault-to-enterprise-opa)
2. [Authenticating Enterprise OPA](#authenticating-enterprise-opa)
3. [Accessing secrets from Vault](#accessing-secrets-from-vault)


### Examples


#### Simple

```yaml
ekm:
  vault:
    url: "http://127.0.0.1:8200"
    access_type: "token"
    token: "dev-only-token"

    license: 
      key: "kv/data/license:data/key"
```

This will configure Enterprise OPA to extract its license key from the Vault key-value store located at `kv/data/license:data/key`


#### Advanced

```yaml
services:
  acmecorp:
    credentials:
      bearer:
        scheme: "bearer"
        token: "${vault(kv/data/acmecorp/bearer:data/token)}"

bundles:
  test:
    service: acmecorp
    resource: bundle.tar.gz
    signing:
      keyid: rsa

discovery:
  service: acmecorp
  resource: discovery.tar.gz
  signing:
    keyid: rsa

ekm:
  vault:
    url: "https://127.0.0.1:8200"
    access_type: "approle"
    approle:
      role_id: "1f918089-5cae-ede9-a60a-e31bae4af08a"
      secret_id: "CAESIMi79bbSmYzNw-pgneGh4KHGh"
      wrapped: true

    license: 
      key: "kv/data/license:data/key"

    keys:
      rsa.key: "kv/data/discovery/rsa:data/key"

    httpsend: 
      https://www.acmecorp.com:
        tls_client_cert: "kv/data/tls/client:data/cert"
        headers:
          Content-Type: "kv/data/tls/client:data/content-type"
          Authorization:
            bearer: "kv/data/tls/bearer:data/token"
            scheme: "kv/data/tls/bearer:data/scheme"
```

In this example, Enterprise OPA will:
- Extract its license key from the Vault key-value store located at `kv/data/license:data/key`
- Use the value looked up from `vault()` for the authorization bearer token used to retrieve the discovery and test bundles from the `acmecorp` service
- Override the RSA public key used for bundle signing with `kv/data/discovery/rsa:data/key`
- Override the `http.send` `tls_client_cert`, Authorization and Content-Type headers for requests to `https://www.acemecorp.com`


### Configuration


#### Connecting Vault to Enterprise OPA

| Field | Type | Required | Description | Environment Variables |
| --- | --- | --- | --- | ---|
| `ekm.vault.url` | `string` | Yes | URL of the Vault server to use. | `VAULT_ADDR` |
| `ekm.vault.insecure` | `string` | No | Enables TLS without certificate verification. | `VAULT_SKIP_VERIFY` |
| `ekm.vault.rootca` | `string` | No | Root certificate authority (TLS certificate verification) | `VAULT_CACERT` or `VAULT_CAPATH`
| `ekm.vault.access_type` | `string` | Yes | Access type determines which Vault authentication service to use. Supported types are `token`, `approle`, `kubernetes` | |


#### Authenticating Enterprise OPA

Configuration is required for the corresponding `ekm.vault.access_type`.


##### token

| Field | Type | Required | Description | Environment Variables |
| --- | --- | --- | --- | --- |
| `ekm.vault.token` | `string` | No | Token to use for token authentication. | `VAULT_TOKEN` |
| `ekm.vault.token_file` | `string` | No | Token file to use for token authentication. | `VAULT_TOKEN` |


##### approle

| Field | Type | Required | Description | Environment Variables |
| --- | --- | --- | --- | --- |
| `ekm.vault.approle.role_id` | `string` | No | Role ID to use for AppRole authentication. | |
| `ekm.vault.approle.secret_id` | `string` | No | Secret ID to use for AppRole authentication. | |
| `ekm.vault.approle.wrapped` | `bool` | No | Secret ID is wrapped. | |


<!-- markdownlint-disable MD044 -->
##### kubernetes
<!-- markdownlint-enable MD044 -->

| Field | Type | Required | Description | Environment Variables |
| --- | --- | --- | --- | --- |
| `ekm.vault.kubernetes.service_token` | `string` | No | The Service access token file path to use for Kubernetes authentication. | |


#### Accessing Secrets from Vault


##### License keys

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `ekm.vault.license.token` | `string` | No | License token to use for license verification. |
| `ekm.vault.license.key` | `string` | No | License key to use for license verification. |


#### Config Values

Since [Enterprise OPA v1.26.0](https://github.com/StyraInc/enterprise-opa/releases/tag/v1.26.0), you can use _variable interpolation_ to put secrets into your configurations of services, keys, and plugins.

For example, to configure the decision logs plugin to use a secret for posting logs to an HTTP endpoint:
```yaml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: http
      url: https://myservice.corp.com/v1/logs
      headers:
        Authorization: "bearer ${vault(kv/data/logs:data/token)}"
```

`${vault(..)}` can be used in strings, or as the sole value of a configurable.

It's valid in the configuration file's _services_, _keys_, and _plugins_ section.

See [the format described below](#note-addressing-vault-data) for possible arguments to `vault()`.


#### Services and Keys Overrides

Services and Keys overrides act exactly like the CLI overrides.
These static string overrides are read once at startup and on any discovery bundle reconfiguration.

See OPA [CLI Runtime Overrides](https://www.openpolicyagent.org/docs/configuration/#cli-runtime-overrides) for details about overrides.


#### `http.send` Overrides

The request parameters and request Headers used in an `http.send` call can be overridden with values from Vault keys.

_Note: Each time `http.send` is executed, it re-reads and re-applies the corresponding URL's `ekm.vault.httpsend` overrides._

See OPA [`http.send`](https://www.openpolicyagent.org/docs/policy-reference/#http) for built-in parameters and details.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `ekm.vault.httpsend.[url].headers.[parameter]` | `string` | No | Create/Override `http.send` Headers [parameter] (ie: Content-Type) string parameters. |
| `ekm.vault.httpsend.[url].headers.[parameter].bearer` | `string` | No | Create/Override `http.send` Headers [parameter] (ie: Authorization) as scheme + ' ' + bearer. |
| `ekm.vault.httpsend.[url].headers.[parameter].scheme` | `string` | No | Create/Override `http.send` Headers [parameter] (ie: Authorization) as scheme + ' ' + bearer. (default `Bearer`) |
| `ekm.vault.httpsend.[url].[parameter]` | `string` | No | Create/Override OPA `http.send` [parameter] (ie: 'URL', 'method', 'body') string parameters. |


## Note: Addressing Vault Data

EKM uses Vault logical reads to retrieve data and each lookup key is specified as a tuple `path:[object/]field` (e.g. `kv/data/license:data/key`) where:
- `path` is the Vault logical path
- `field` is the field in the object response; Logical reads can return objects or an object of objects.
- `object/` is the object wrapper (optional). _Note: The Vault KV V2 engine wraps all logical read responses with a `data/` object_
