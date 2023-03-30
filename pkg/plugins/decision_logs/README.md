# Decision Log Replacement

Load allows for replacing the built-in decision logs plugin by a custom one:

```yaml
services:
- name: knownservice
  url: "http://knownservice/prefix"
  response_header_timeout_seconds: 12
decision_logs:
  service: servicefoo
```

would become

```yaml
services:
- name: knownservice
  url: "http://knownservice/prefix"
  response_header_timeout_seconds: 12
plugins:
  load_decision_logger:
    output:
      type: service
      service: knownservice
      resource: decisionlogs
```

Configuring both the default and the replacement logger will cause a failure.

The replacement logger currently supports `type: service` and `type: console`, with more
to come. Furthermore, `type: http` will allow for controlling more of the payload format
and batching options then `type: service`. The latter is for compatibility with OPA's DL
plugin.


###  Buffering

The new logger supportes three kinds of buffers:
- `unbuffered`: responses aren't returned until the DL entry of the decision has
  successfully been written to a sink.
- `memory`: DL entries are buffered in memory
- `disk`: DL entries are buffered on disk. This would survive a service restart.

A buffer config looks like this:
```yaml
buffer:
  type: memory
  max_bytes: 120
  flush_count: 100
  flush_period: 10s
  flush_bytes: 12
```

(Where the values uses here come from testing, and shouldn't be used in real life.)

### TODOs

- carry over bearer auth, mTLS from services
- expose more batching options
- add further sink options

### Differences from the default plugin

These may become TODOs if we decide that the deviation isn't acceptable.

- Response bodies don't contain `{"decision_id": "some-uuid-4"}`.
  We could add it back, or add a response header.
- Some DL payload fields aren't set: type, mapped_results, ...
  We can look into adding them if DAS requires them, or some sink needs them.
- Console output goes to stdout, whereas with the default plugin, it goes to
  stderr. All of Load's logs to go stderr, so it's actually rather convenient
  to collect DLs from stdout instead.