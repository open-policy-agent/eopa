# Changelog

## v1.9.1, v1.9.2

These releases have been release engineering fixes to sort out automated publishing of this changelog, capabilities JSON files, and gRPC protobuf definitions.

## v1.9.0

This release updates the OPA version used in Enterprise OPA to [v0.56.0](https://github.com/open-policy-agent/opa/releases/tag/v0.56.0), and integrates several bugfixes and new features.

### `mongodb.find`, `mongodb.find_one`: query MongoDB databases during policy evaluation

Enterprise OPA now supports querying MongoDB databases!

Two new builtins are dedicated for this purpose: `mongodb.find`, and `mongodb.find_one`. These correspond approximately to MongoDB's [`db.collection.find()`](https://www.mongodb.com/docs/manual/reference/method/db.collection.find/) and [`db.collection.findOne()`](https://www.mongodb.com/docs/manual/reference/method/db.collection.findOne/) operations, respectively. These operations make it possible to integrate MongoDB databases efficiently into policies, depending on whether a single or multiple document lookup is needed.

Find out more in the new [Tutorial](https://docs.styra.com/enterprise-opa/tutorials/querying-mongodb), or see the [Reference documentation](https://docs.styra.com/enterprise-opa/reference/built-in-functions/mongodb) for more details.

### `dyanmodb.send`: query DynamoDB during policy evaluation

This builtin currently supports sending [GetItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_GetItem.html) and [Query](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_Query.html) requests to a DynamoDB endpoint, allowing direct integration of DynamoDB into policies.

Find out more in the new [Tutorial](https://docs.styra.com/enterprise-opa/tutorials/dynamodb-send), or see the [Reference documentation](https://docs.styra.com/enterprise-opa/reference/built-in-functions/dynamodb) for more details.

### `vault.send` for interacting directly with Hashicorp Vault in policies.

This new builtin provides support for more direct, request-oriented Hashicorp Vault integrations in policies than was previously possible through the [EKM Plugin](https://docs.styra.com/enterprise-opa/reference/configuration/using-secrets/from-hashicorp-vault).

See the [Reference documentation](https://docs.styra.com/enterprise-opa/reference/built-in-functions/vault) for more details.

### gRPC plugin Decision Logs Support

The gRPC server plugin now integrates into Enterprise OPA's decision logging!
This means that gRPC requests are logged in a near-identical format to HTTP requests, allowing deeper insight into the usage and performance of Enterprise OPA deployments in production.

## v1.8.0

This release updates the OPA version used in Enterprise OPA to
[v0.55.0](https://github.com/open-policy-agent/opa/releases/tag/v0.55.0).

## v1.7.0

### Envoy External Authorization Support

This release makes Styra Enterprise OPA a drop-in replacement for
[opa-envoy-plugin](https://github.com/open-policy-agent/opa-envoy-plugin/), to be
used with the [External Authorization feature](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/security/ext_authz_filter.html)
of the popular Envoy API gateway, and Envoy-based service meshes such as Istio
and Gloo Edge.

It works exactly like `opa-envoy-plugin``, i.e. the images known as `openpolicyagent/opa:latest-envoy`,
but featuring all the Enterprise OPA enhancements.

[See here](https://www.openpolicyagent.org/docs/latest/envoy-introduction/) for a general
introduction to OPA and Envoy.

### Enhanced OpenTelemetry Support

Styra Enterprise OPA now supports OpenTelemetry Traces for the following operations:

- Rego VM evaluations, with extra spans for `http.send` and `sql.send`
- All decision log operations.
- All of its gRPC handlers.

This allows for improved observability, allowing you to quicker pin-point any issues
in your distributed authorization system.

## v1.6.0

This release updates the OPA version used in Styra Enterprise OPA to
[v0.54.0](https://github.com/open-policy-agent/opa/releases/tag/v0.54.0),
along with gRPC plugin improvements and new gRPC streaming endpoints.

### Support for large gRPC message sizes

Most gRPC implementations default to having a max receivable message size of 4 MB for both servers and clients.
This helps avoid memory exhaustion from large messages sent by misconfigured or malicious actors on the other side of the connection.

This size limit presents a problem for Enterprise OPA though: a relatively simple rule query that returns a large amount of data can easily break past that 4 MB message size limit.
Additionally, clients who need to provide more than 4 MB of data for a data update or rule query input can also run into the receivable message size limit.
To work around this problem, we have to attack it from both the client and server sides.

On the client side, most gRPC implementations allow providing the "Max Receive Message Size" as a parameter for the gRPC call. (See the [`CallOption.MaxRecvMsgSize`](https://pkg.go.dev/google.golang.org/grpc#MaxCallRecvMsgSize) option in Go, for example.)
This means that clients who want to receive potentially massive responses from the Enterprise OPA server will need to do a little more setup at call time, but don't necessarily need to change their Enterprise OPA configs.

For the server side of the problem, we changed Enterprise OPA to support a new configuration option for the gRPC plugin: `grpc.max_recv_message_size`

In the example configuration below, we start up the Enterprise OPA gRPC server on `localhost:9090`, and set it to receive messages up to 8 MB in size:
```yaml
plugins:
  grpc:
    # 8 MB, in bytes:
    max_recv_message_size: 8589934592
    addr: "localhost:9090"
```

This allows the server to receive larger gRPC messages from clients than before.

Fixing both sides of the large gRPC message size problem allow for high-throughput and data-heavy use cases over the gRPC API that were not possible before.

### New streaming gRPC endpoints for the Data and Policy APIs

The Data and Policy gRPC services now provide bidirectional streaming endpoints!
These endpoints work similarly to the experimental `BulkRW` endpoint that was explored in earlier versions of Enterprise OPA, and provide several speed and efficiency benefits over the REST API or existing unary gRPC endpoints.

They provide fixed-structure transactions that describe batches of CRUD operations, where all "write" operations (that would cause changes to the Data and Policy stores) are run as a single, sequential write transaction first, and then all "read" operations, such as rule queries, are evaluated in parallel.
If any write operations fail, the entire request fails.
Read operations have their failures reported as normal response messages with error-wrapping message types.

These batched operations can provide substantial throughput improvements over using the existing unary gRPC endpoints:
 - The connection cost is paid once at the start of the stream, instead of once per call.
 - Read and write operations enjoy greatly reduced contention for access to the Data and Policy stores.
 - Some operations are able to be parallelized.

For styles of API access where the successes and failures of write operations should be independent of one another, callers can send several smaller messages over the stream, and will receive back individual successful responses, or error messages for each failure that occurs.

See the [Enterprise OPA gRPC docs](https://buf.build/styra/enterprise-opa) on the Buf Schema Registry for more details.

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
