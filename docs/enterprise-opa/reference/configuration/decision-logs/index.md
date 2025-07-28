---
title: Decision Logs Configuration | Enterprise OPA
---

Enterprise OPA has expanded the OPA decision logger, with support for:
1. Multiple sinks, including native external integrations such as [Splunk](./decision-logs/splunk).
2. Configurable log write buffering behavior.


## Common Configuration

Enhanced decision logs are provided by a decision logger _plugin_ called `eopa_dl`.

:::note
The Enterprise OPA decision logger _must_ be used together with the default
decision log plugin in OPA.

If the OPA decision logger plugin is not configured, Enterprise OPA will error and exit.
:::

```yaml
decision_logs:
  plugin: eopa_dl
  drop_decision: /system/log/drop
  mask_decision: /system/log/mask
plugins:
  eopa_dl:
    buffer:
      type: disk # one of "memory" (default), "disk" and "unbuffered"
      path: /var/tmp/dl.db
    output:
    - type: console
    # any number of further outputs, see individual sink configurations
```


### Buffering

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `buffer.type` | `string` | No | `memory` | One of `memory`, `disk`, or `unbuffered` |


#### `memory`

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `buffer.max_bytes` | `int` | Yes | 524288000 _(i.e. 500M)_ | Maximum buffer size (in bytes) to allow before applying backpressure upstream.  |

One of the following must also be configured.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `buffer.flush_at_count` | `int` | No | 0 | Number of messages at which the batch should be flushed. If 0 disables count based batching.  |
| `buffer.flush_at_bytes` | `int` | No | 0 | Amount of bytes at which the batch should be flushed. If 0 disables size based batching.  |
| `buffer.flush_at_period` | `string` | No | | Period in which an incomplete batch should be flushed regardless of its size (e.g. 1s).  |

The memory buffer behaves in much the same way as OPA's decision logging, except
that drop and mask decisions are applied _asynchronously_.
This allows for faster API responses even with decision logging enabled.


#### `disk`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `buffer.path` | `string` | Yes | The location of the buffer on disk  |

Disk is an on-disk buffer. It is slower than memory buffering, but is
generally more robust, and safe across service restarts.


#### `unbuffered`

When using an unbuffered decision log, no API response will be sent until the decision has
successfully been written to every configured sink. This is the slowest option, but guarantees that decision logs have been written. This can be useful in systems with strict auditability requirements.

:::warning
Do not combine `unbuffered`  with a sink that uses batching: all API responses will be
stalled until a batch is ready to be flushed.
:::


## Common Output Configuration

Several decision log outputs share common configuration options.


### TLS

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.tls.cert` | `string` | Yes | | Path to public certificate that matches the private key.  |
| `output.tls.private_key` | `string` | Yes | | Path to private key used to decrypt messages. |
| `output.tls.ca_cert` | `string` | Yes | | Path to public certificate of the certificate authority. |
| `output.tls.skip_cert_verify` | `bool` | No | false | Skip certificate verification. |


### Batching

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.batching.array` | `bool` | No | false | Send batches as arrays of JSON blobs.  |
| `output.batching.compress` | `bool` | No | false | Compress output with gzip. |

One of the following must also be configured.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.batching.at_count`  | `int` | No | 0 | Number of messages at which the batch should be flushed. If 0 disables count based batching.  |
| `output.batching.at_bytes` | `int` | No | 0 | Amount of bytes at which the batch should be flushed. If 0 disables size based batching.  |
| `output.batching.at_period` | `string` | No | | Period in which an incomplete batch should be flushed regardless of its size (e.g. 1s).  |


### Mask and Drop decisions

Mask and drop decisions are also supported per-output.
The configured decision will only be evaluated for the output it's configured on.
This can be used to log all decisions _unmasked_ to a local sink, and also send
a sample (via `drop_decision`) of masked (via `mask_decision`) decisions to a remote
sink.

See the [OPA docs for details on mask and drop policies](https://www.openpolicyagent.org/docs/edge/management-decision-logs/).

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `output.mask_decision` | `string` | No | Decision to evaluate for masking decision logs (e.g. `/system/log/mask` for `data.system.log.mask`). |
| `output.drop_decision` | `string` | No | Decision to evaluate for dropping decision logs (e.g. `/system/log/drop` for `data.system.log.drop`). |
