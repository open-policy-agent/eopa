---
sidebar_position: 1
sidebar_label: Splunk Sink
title: Splunk Sink Configuration | EOPA
---

The Splunk decision log sink allows publishing decision log entries as
events to a Splunk HTTP Endpoint Collector.


## Example Configuration

```yaml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: splunk
      url: https://YOUR-TENANT.splunkcloud.com:8088/services/collector/event
      token: $SPLUNK_TOKEN
      batching:
        at_period: "10s" # flush batch every 10 seconds
        at_count: 10     # flush batch every 10 log entries
        at_bytes: 10240  # flush batch whenever 10240 bytes are exceeded
        compress: true   # use gzip on payloads (default: false)
      tls:
        cert: path/to/cert.pem
        private_key: path/to/key.pem
        ca_cert: path/to/ca.pem
        skip_cert_verify: false # default false
```

Decision logs will be batched according to your configuration, and sent to Splunk
in its desired format, i.e. wrapped in an `event` envelope:

```json
{
  "event": {
    "decision_id": "955ee45b-8624-4e23-af67-e3513d69c997",
    "input": {
      "method": "GET",
      "path": "/data/fruits"
    },
    "labels": {
      "id": "6067027a-caf0-4601-8691-6a1ba0906b4b",
      "type": "eopa",
      "version": "0.52.0"
    },
    "metrics": {
      "counter_regovm_eval_instructions": 42,
      "counter_server_query_cache_hit": 1,
      "timer_rego_input_parse_ns": 17637,
      "timer_regovm_eval_ns": 73731,
      "timer_server_handler_ns": 110230
    },
    "nd_builtin_cache": {},
    "path": "authz",
    "req_id": 4,
    "requested_by": "127.0.0.1:61318",
    "result": {
      "allow": true
    },
    "timestamp": "2023-05-12T13:36:37.496602+02:00"
  },
  "time": 1683891397
}
```

:::tip
You can use the EOPA External Key Management feature to avoid putting your Splunk token secret into the configuration file.
[Learn more.](../using-secrets/from-hashicorp-vault)
:::

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.url` | `string` | Yes |  | Address to connect to Splunk. |
| `output.token` | `string` | Yes |  | Splunk event collector token. |
| `output.tls` | Object | No |  | See [TLS configuration](../decision-logs#tls) |
| `output.batching` | Object | No |  | See [Batching configuration](../decision-logs#batching) |
