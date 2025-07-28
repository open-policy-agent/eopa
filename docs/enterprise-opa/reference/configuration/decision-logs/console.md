---
sidebar_position: 5
sidebar_label: Console Sink
sidebar_class_name: divider-after
title: Console Sink Configuration | Enterprise OPA
---

The Console decision log sink is an expansion on the [OPA console log sink](https://www.openpolicyagent.org/docs/management-decision-logs/#local-decision-logs).

```yaml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: console
```

It takes no further configuration, and will print decision logs to
the Enterprise OPA agent's standard output.
