## Changelog

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