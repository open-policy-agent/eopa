## Changelog


### v0.102.1, v0.102.2

These releases have been release engineering fixes to sort out macos binary signing
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