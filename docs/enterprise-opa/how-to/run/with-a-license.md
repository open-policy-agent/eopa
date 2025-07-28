---
sidebar_position: 1
sidebar_label: Run with a license
title: How to run Enterprise OPA with a license
---

<!-- markdownlint-disable MD044 -->
import LicenseTrialAdmonition from '../../_license-trial-admonition.md';

To run Enterprise OPA with plugins and performance optimizations, a license is required; if no license is provided, Enterprise OPA will enter a [fallback mode](#fallback-mode).

For existing customers, please contact your customer support representative for a copy your license.

<LicenseTrialAdmonition />


## Using an Online License Key

To use a license key, you must set the `EOPA_LICENSE_KEY` environment variable before running `eopa`, e.g.

```shell
# terminal-command
export EOPA_LICENSE_KEY=...
# terminal-command
eopa run ...
```

Refer to the [`eopa run` CLI reference](/enterprise-opa/reference/cli-reference#eopa-run) for a full list of options


## Using an Offline License File

Offline license files are only available for paid licenses. Please contact your customer support representative for a copy of your offline license file.

To use the license file, you must set the `EOPA_LICENSE_TOKEN` environment variable to the location of the license file, e.g.

```shell
# terminal-command
export EOPA_LICENSE_TOKEN=<path/to/license/file>
# terminal-command
eopa run ...
```

Refer to the [`eopa run` CLI reference](/enterprise-opa/reference/cli-reference#eopa-run) for a full list of options


## Fallback Mode

If you fail to provide a license or your license has expired, Enterprise OPA will default to running in a fallback mode. The functionality in this mode is **equal to the behavior** of the open-source Open Policy Agent.

If Enterprise OPA runs in fallback mode, a WARN log message will be emitted during startup that looks like:

```log
{"level":"warning","msg":"Switching to OPA mode. Enterprise OPA functionality will be disabled.","time":"2024-11-21T09:28:13-08:00"}
```

If you wish to disable fallback mode, use the `--no-license-fallback` configuration when running Enterprise OPA. For the next 48 hours, Enterprise OPA
will periodically attempt to validate the provided license. At the end of the grace period, it will shut down. A WARN log message will be emitted during startup that contains a message about retries, like `retrying for 47h59m0s before shutdown`:

```log
{"level":"warning","msg":"no license provided\n\nSign up for a free trial now by running `eopa license trial`\n\nIf you already have a license:\n    Define either \"EOPA_LICENSE_KEY\" or \"EOPA_LICENSE_TOKEN\" in your environment\n        - or -\n    Provide the `--license-key` or `--license-token` flag when running a command\n\nFor more information on licensing Enterprise OPA visit https://docs.styra.com/enterprise-opa/installation/licensing: retrying for 47h59m0s before shutdown","time":"2024-11-21T09:52:41-08:00"}
```
