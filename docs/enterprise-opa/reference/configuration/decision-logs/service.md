---
sidebar_position: 4
sidebar_label: Service Sink
title: Service Sink Configuration | Enterprise OPA
---

The Service decision log sink is an expansion on the [OPA service decision log sink](https://www.openpolicyagent.org/docs/management-decision-logs/). It sends decision logs as payloads to an HTTP API.

:::note
The payload sent to the configured HTTP service is the same as it is in OPA.
:::


## Example Configuration

```yaml
services:
- name: dl
  url: https://logservice.internal/post
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: service
      service: dl
```
Please see [the OPA service configuration documentation](https://www.openpolicyagent.org/docs/configuration/#services)
for all details about the `services` configuration section.

The following configuration fields are supported for the `services` section:
- `services[_].timeout`
- `services[_].headers`
- `services[_].oauth2`
- `services[_].tls`
