---
sidebar_position: 2
sidebar_label: Environment Variables
title: Using environment variables as secrets | Enterprise OPA
---


# Using environment variables as secrets


## During Runtime

Enterprise OPA supports using environment variables for static runtime configuration.

See the [Open Policy Agent documentation](https://www.openpolicyagent.org/docs/configuration/#using-environment-variables-in-configuration) for reference.


## During Discovery

:::info
This feature is only supported in Enterprise OPA.
:::

Enterprise OPA supports using environment variables in static JSON configuration files delivered via [discovery](https://www.openpolicyagent.org/docs/management-discovery/).

This feature makes discovery configuration and runtime configuration equivalent.
