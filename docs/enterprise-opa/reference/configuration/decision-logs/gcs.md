---
sidebar_position: 4
sidebar_label: Google Cloud Storage Sink
title: Google Cloud Storage Sink Configuration | Enterprise OPA
---

The Google Cloud Storage decision log sink allows publishing decision log entries as
JSON files to Google Cloud Storage.
It is configured by creating a sink with an `output.type` of `gcs`.


## Example Configuration

```yaml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: gcs
      bucket: logs
      credentials_json_file: /root/gcp.credentials.json
      timeout: "10s"
      batching:
        at_period: "10s" # flush batch every 10 seconds
        at_count: 10     # flush batch every 10 log entries
        at_bytes: 10240  # flush batch whenever 10240 bytes are exceeded
```

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `output.bucket` | `string` | Yes |  | The bucket where logs are sent to. |
| `output.credentials_json_file` | `string` | No |  | Location of your Google cloud credentials JSON file. Defaults to the environment variable `GOOGLE_APPLICATION_CREDENTIALS`. |
| `output.timeout` | `string` | No |  | Timeout (e.g. `10s`) |
| `output.batching` | Object | No |  | See [Batching configuration](../decision-logs#batching) |
