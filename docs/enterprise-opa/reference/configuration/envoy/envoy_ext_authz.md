---
sidebar_position: 1
sidebar_label: Integrating with Envoy
title: Integrating with Envoy | Enterprise OPA
---


# Integrating with Envoy

Enterprise OPA can be used with Envoy as an [External Authorization filter](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/security/ext_authz_filter.html), and supports configuration in the same manner as the open source [OPA-Envoy plugin](https://github.com/open-policy-agent/opa-envoy-plugin).


## Configuration

Enterprise OPA supports all of the same configuration options from OPA-Envoy.
An example configuration snippet enabling the plugin and several of its features is shown below:

```yaml
plugins:
  envoy_ext_authz_grpc:
    addr: :9191 # default `:9191`
    path: envoy/authz/allow # default: `envoy/authz/allow`
    dry-run: false # default: false
    enable-reflection: false # default: false
    grpc-max-recv-msg-size: 40194304 # default: 1024 * 1024 * 4
    grpc-max-send-msg-size: 2147483647 # default: max Int
    skip-request-body-parse: false # default: false
    enable-performance-metrics: false # default: false. Adds `grpc_request_duration_seconds` prometheus histogram metric
```

:::note
See the [OPA-Envoy docs](https://www.openpolicyagent.org/docs/envoy-introduction/) for additional information.
:::
