## Changelog

### v1.0.0

This release marks the first general availability release of Styra Load.
Load provides a number of improvements over open source OPA, including:

 - Optimizations (CPU/Memory use)
 - Datasource integrations
 - Live Impact Analysis

### v0.102.5

 * This release is a release engineering fix to sort out part of our gRPC documentation system.

### v0.102.4

* Fix `--disable-telemetry` being ignored for `load run --server`.
* Use `google.protobuf.Value` and `google.protobuf.Struct` in the gRPC API instead of raw JSON strings.
* Further performance improvements to the Rego VM and bundle loading.

### v0.102.3

* Fix `load bundle convert` regression

### v0.102.1, v0.102.2

These releases have been release engineering fixes to sort out MacOS binary signing
of published executables.

### v0.102.0

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

### v0.101.1

 * Fixed a hang triggered by sending the gRPC `BulkRW` endpoint multiple blank messages in sequence.

### v0.101.0

* Updated the internal OPA version to v0.50.0.
  See the [OPA Release Notes](https://github.com/open-policy-agent/opa/releases/tag/v0.50.0) for details.
* Live Impact Analysis can now be used from the CLI: `load liactl record`. See `load liactl help record`.
* Performance improvements to the Rego VM.
* Capabilities: Load now includes OPA-compatible capabilities data.
* Build: Load container images now include SBOM data.
* Various other third-party dependency bumps.
