# Changelog

## v1.5.1

This release fixes a typographical error in the protobuf markdown introduced when renaming Enterprise OPA.

## v1.5.0

This release changes the name of Styra Load to Styra Enterprise OPA.

## v1.4.1

This release updates the OPA version used in Styra Load to
[v0.53.1](https://github.com/open-policy-agent/opa/releases/tag/v0.53.1).

## v1.4.0

This release features a long-sought-after new built-in: `sql.send`!

### `sql.send`: query databases during policy evaluation

The new builtin in Styra Load, `sql.send`, can be considered `http.send`'s relational cousin:
It allows you to run any kind of custom query against a relational database management system, including

1. PostgreSQL
2. MySQL
3. SQLite

This is an example call, querying a SQLite database with a parametrized SQL query:
```rego
subordinate := sql.send({
	"driver": "sqlite",
	"data_source_name": "sqlite://data/company.db",
	"query": "SELECT * FROM subordinates WHERE manager = $1 AND subordinate = $2",
	"args": [input.user, username],
})
count(subordinate.rows) > 0 # Make sure the row exists in the subordinates table.
```

Just like `http.send`, it allows you to pull in the most recent data you have in your database
when it's relevant for your policy decision.

[Find out more in the new tutorial.](https://docs.styra.com/load/tutorials/abac-with-sql)

### Export decision logs to S3

You can now send decision logs to S3-compatible stores.

[Find out more in the new tutorial.](https://docs.styra.com/load/tutorials/decision-logs/s3)

### CLI-based trial sign up

It is now possible to sign up for a free trial directly through the Styra Load CLI.
Running `load license trial` will collect all required information and generate a
new license key, which can be used to activate Styra Load immediately.

### OPA v0.53.0

This release includes OPA v0.53.0. [See the release notes for details.](https://github.com/open-policy-agent/opa/releases/tag/v0.53.0)

## v1.3.0

This release unveils two new feature sets, and includes some smaller quality-of-life improvements:

### External Key Management (EKM) via Hashicorp Vault

The Styra Load Vault integration can be used to:

* Retrieve the Styra Load License key from a Vault secret
* Override the configuration for a Styra Load service or key configuration
* Override the configuration of `http.send`

[See the documentation for more details.](https://docs.styra.com/load/configuration/using-secrets/from-hashicorp-vault)

### Enhanced Decision Logging

Styra Load now features its own decision logging infrastructure!
It gives you extra flexibility, and a multitude of new sinks, including
**Apache Kafka** and **Splunk**.

[Find out more about this by following the tutorial.](https://docs.styra.com/load/tutorials/decision-logs)

### More Enhancements

* The [Git data source](https://docs.styra.com/load/configuration/data/git) now allows configuring a `branch`.
* The [S3 data source](https://docs.styra.com/load/configuration/data/s3) now lets you provide an `endpoint`.
   This enables you to work with other S3-compatible APIs, like MinIO.


## v1.2.0

This release contains an update to the [latest version of OPA][gh-opa-v52] (`v0.52.0`), as well as bugfixes and performance improvements.

### CLI

 - LIA: Output now displays time values in human-friendly units, instead of always nanoseconds.

### Runtime

 - Small performance improvements around internal string caching.

### Fixes

 - Improved logging around licensing errors.
 - `data`: Plugin now detects and errors when a [bundle's roots][opa-bundle-format] would clash with the namespace owned by a [`data` plugin][load-data-plugins-reference].

   [gh-opa-v52]: https://github.com/open-policy-agent/opa/releases/tag/v0.52.0
   [opa-bundle-format]: https://www.openpolicyagent.org/docs/latest/management-bundles/#bundle-file-format
   [load-data-plugins-reference]: https://docs.styra.com/load/configuration/data/


## v1.1.0

This release includes a host of runtime performance improvements, bugfixes, and a new gRPC plugin.
Startup times have also been dramatically improved over older releases, thanks to upstream fixes in some of our dependencies.

### New protocol support via the `grpc` plugin

Load now supports gRPC versions of OPA's [Policy][opa-rest-policy] and [Data][opa-rest-data] [REST APIs][opa-rest-docs], as well as a new experimental bulk operations API.
The gRPC server is enabled via the `grpc` plugin.

   [opa-rest-policy]: https://www.openpolicyagent.org/docs/latest/rest-api/#policy-api
   [opa-rest-data]: https://www.openpolicyagent.org/docs/latest/rest-api/#data-api
   [opa-rest-docs]: https://www.openpolicyagent.org/docs/latest/rest-api/

The plugin can be enabled in your Load config file like so:

```yaml
plugins:
  grpc:
    addr: ":9090"
```

Or if you prefer the CLI, try: `load run -s --set plugins.grpc.addr=:9090`

In addition to the normal Load HTTP server, this will start up an unsecured gRPC server on the port you specified in the plugin's options.
This mode is great for testing with tools like [grpcurl][grpcurl], but we strongly recommend that you protect your gRPC server using one of the TLS options detailed below if you intend to make the gRPC port visible to other systems.

   [grpcurl]: https://github.com/fullstorydev/grpcurl

#### TLS Support

To secure the gRPC server, server-side TLS support is available.
Given the files `cert.pem` and `key.pem`, you could configure your Load instance to secure your gRPC connections like so:

```yaml
plugins:
  grpc:
    addr: ":9090"
    tls:
      cert_file: "cert.pem"
      cert_key_file: "key.pem"
```

#### mTLS Support

For additional security, mutual TLS (mTLS) connections can be used, where the client must present a certificate signed by the same Root CA as the server's certificate.
Given the root CA file `ca.pem`, we can add on to the configuration example for server-side TLS, and require clients to authenticate themselves using mTLS:

```yaml
plugins:
  grpc:
    addr: ":9090"
    authentication: "tls"
    tls:
      cert_file: "cert.pem"
      cert_key_file: "key.pem"
      ca_cert_file: "ca.pem"
```

Any client whose certificate was signed with `ca.pem` will be able to authenticate to the server.
All others will get disconnections or TLS errors.

### Runtime

 - Improved iteration speeds over large Rego Object types.
 - Improved memory efficiency via interning for some types.

### Fixes

 - Fixed a minor Rego incompatibility to match OPA's behavior.

## v1.0.1

* Performance improvements for queries of "all of `data`", like `load eval [...] data` or
  `GET /v1/data` with Load's API.
* Fix bug when referencing a bundle via `load eval bundle.tar.gz` (without explicitly loading it
  as a bundle via `-b`). This ensures compatibility with how OPA operates in these circumstances.
* Restructure parts of the gRPC API to make it more resource-focussed.
* Change the exit code for license validation related errors from 2 to 3 -- to differentiate them
  from any other errors.

## v1.0.0

This release marks the first general availability release of Styra Load.
Load provides a number of improvements over open source OPA, including:

 - Optimizations (CPU/Memory use)
 - Datasource integrations
 - Live Impact Analysis

## v0.102.5

 * This release is a release engineering fix to sort out part of our gRPC documentation system.

## v0.102.4

* Fix `--disable-telemetry` being ignored for `load run --server`.
* Use `google.protobuf.Value` and `google.protobuf.Struct` in the gRPC API instead of raw JSON strings.
* Further performance improvements to the Rego VM and bundle loading.

## v0.102.3

* Fix `load bundle convert` regression

## v0.102.1, v0.102.2

These releases have been release engineering fixes to sort out MacOS binary signing
of published executables.

## v0.102.0

* `load eval` now has a CLI flag for changing the instruction limit.
* Various BJSON bundle loading issues have been identified and fixed.
* Data paths controlled by data plugins are now protected from manual
  updates via the API.
* `load version` has been revamped.
* Windows users may have a better CLI experience now, as a
  superfluous user information lookup has been removed.
* Further performance improvements to the Rego VM.
* Updated the internal OPA version to v0.50.2.
* Various other third-party dependency bumps.

## v0.101.1

 * Fixed a hang triggered by sending the gRPC `BulkRW` endpoint multiple blank messages in sequence.

## v0.101.0

* Updated the internal OPA version to v0.50.0.
  See the [OPA Release Notes](https://github.com/open-policy-agent/opa/releases/tag/v0.50.0) for details.
* Live Impact Analysis can now be used from the CLI: `load liactl record`. See `load liactl help record`.
* Performance improvements to the Rego VM.
* Capabilities: Load now includes OPA-compatible capabilities data.
* Build: Load container images now include SBOM data.
* Various other third-party dependency bumps.
