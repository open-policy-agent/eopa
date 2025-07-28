---
sidebar_position: 2
sidebar_label: Kafka Sink
title: Kafka Sink Configuration | Enterprise OPA
---

The Kafka decision log sink allows publishing decision log entries as
messages on a Kafka topic. It is configured by creating a sink with an `output.type` of `kafka`.


## Example Configuration

```yaml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: kafka
      urls:
      - kafka.internal:9092
      topic: logs
      timeout: "10s"
      tls:
        cert: path/to/cert.pem
        private_key: path/to/key.pem
        ca_cert: path/to/ca.pem
        skip_cert_verify: false # default false
      batching:
        at_period: "10s" # flush batch every 10 seconds
        at_count: 10     # flush batch every 10 log entries
        at_bytes: 10240  # flush batch whenever 10240 bytes are exceeded
        array: true      # combine log entries into JSON array (default: lines of JSON objects)
        compress: true   # compress payload with gzip (default: false)
```

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.urls` | Array of `string` | Yes |  | Address to connect to Kafka. |
| `output.topic` | `string` | Yes |  | The Kafka topic where logs are written. |
| `output.timeout` | `string` | No |  | Timeout (e.g. `10s`) |
| `output.tls` | Object | No |  | See [TLS configuration](../decision-logs#tls) |
| `output.batching` | Object | No |  | See [Batching configuration](../decision-logs#batching) |
