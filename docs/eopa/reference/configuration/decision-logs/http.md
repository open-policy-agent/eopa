---
sidebar_position: 4
sidebar_label: HTTP Sink
title: HTTP Sink Configuration | EOPA
---

The HTTP decision log sink is a more generic version of the Service sink. It will send decision logs as payloads to an HTTP API.

:::note
The payload sent to the configured HTTP service is the same as it is in OPA.
:::


## Example Configuration

```yaml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: http
      url: https://mytenant.example.com/v1/logs
      retry:
        period: 1s
        max_attempts: 10
      rate_limit:
        count: 500
        interval: 1s
```

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.url` | `string` | Yes |  | URL of the HTTP API to send decision logs |
| `output.timeout` | `string` | No |  | Timeout (e.g. `10s`) |
| `output.headers` | Object | No |  | See [OPA service configuration documentation](https://www.openpolicyagent.org/docs/configuration/#services) | |
| `output.oauth2` | Object | No |  | See [OPA service configuration documentation](https://www.openpolicyagent.org/docs/configuration/#services) | |
| `output.retry` | Object | No |  | See [Retry configuration](#retry) |
| `output.rate_limit` | Object | No |  | See [Rate Limit configuration](#rate-limit) |
| `output.tls` | Object | No |  | See [TLS configuration](../decision-logs#tls) |
| `output.batching` | Object | No |  | See [Batching configuration](../decision-logs#batching) |
| `output.mask_decision` | `string` | No | | See [Mask and Drop decisions configuration](../decision-logs#mask-and-drop-decisions) |
| `output.drop_decision` | `string` | No | | See [Mask and Drop decisions configuration](../decision-logs#mask-and-drop-decisions) |


### OAuth2

EOPA will authenticate using a bearer token obtained through the OAuth2 client credentials flow.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.oauth2.enabled` | `bool` | No | False | Enables OAuth2 authentication |
| `output.oauth2.token_url` | `string` | Yes |  | See [OPA service configuration documentation](https://www.openpolicyagent.org/docs/configuration/#oauth2-client-credentials) |
| `output.oauth2.client_key` | `string` | Yes |  | See [OPA service configuration documentation for `client_id`](https://www.openpolicyagent.org/docs/configuration/#oauth2-client-credentials) |
| `output.oauth2.client_secret` | `string` | Yes |  | See [OPA service configuration documentation](https://www.openpolicyagent.org/docs/configuration/#oauth2-client-credentials) |
| `output.oauth2.scopes` | Array of `string` | No |  | See [OPA service configuration documentation](https://www.openpolicyagent.org/docs/configuration/#oauth2-client-credentials) |


### Retry

These configurations allow you to control the underlying retry logic for Benthos.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.retry.period` | `string` | No | See description | ["The base period to wait between failed requests"](https://www.benthos.dev/docs/components/processors/http#retry_period) |
| `output.retry.max_attempts` | `int` | No | See description | ["The maximum number of retry attempts to make"](https://www.benthos.dev/docs/components/processors/http#retries) |
| `output.retry.max_backoff` | `string` | No | See description | ["The maximum period to wait between failed requests"](https://www.benthos.dev/docs/components/processors/http#max_retry_backoff) |
| `output.retry.backoff_on` | Array of `int` | No |See description | ["A list of status codes whereby the request should be considered to have failed and retries should be attempted, but the period between them should be increased gradually.](https://www.benthos.dev/docs/components/processors/http#backoff_on) |
| `output.retry.drop_on` | Array of `int` | No | See description | ["A list of status codes whereby the request should be considered to have failed but retries should not be attempted. This is useful for preventing wasted retries for requests that will never succeed. Note that with these status codes the request is dropped, but message that caused the request will not be dropped."](https://www.benthos.dev/docs/components/processors/http#drop_on) |
| `output.retry.successful_on` | Array of `int` | No | See description | ["A list of status codes whereby the attempt should be considered successful, this is useful for dropping requests that return non-2XX codes indicating that the message has been dealt with, such as a 303 See Other or a 409 Conflict. All 2XX codes are considered successful unless they are present within backoff_on or drop_on, regardless of this field."](https://www.benthos.dev/docs/components/processors/http#successful_on) |


### Rate Limit

These configurations allow you to control the underlying [rate limiting logic for Benthos](https://www.benthos.dev/docs/components/rate_limits/about/).

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.rate_limit.count` | `int` | No | | Maximum requests per time period described in `.interval`  |
| `output.rate_limit.interval` | `string` | No | | The interval over which requests are measured (e.g. `1s`) |

Example configuration to rate limit to 500 requests per second.

```yaml
count: 500
interval: 1s
```
