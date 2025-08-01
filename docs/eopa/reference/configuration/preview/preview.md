---
sidebar_position: 1
sidebar_label: Preview
title: Preview API | EOPA
---

# EOPA Preview

EOPA preview exposes API endpoints for trying out new or updated policies and data with or without existing production policies and data. Evaluations using this API do not affect policies and data being used in the production path.


## Configuration

The Preview API is enabled by default, but can be disabled through configuration. This is supported both at start up and through discovery bundle updates. Define the preview block at the top level of your EOPA configuration file.

```yaml
preview:
  enabled: false
```

The boolean parameter `enabled` is the only available option. Control over the behavior of this API is at the request level, allowing each call to behave differently depending on the needs of the requesting party.

Review the [preview API reference](/eopa/reference/api-reference/preview-api) for information on making preview API requests as well as many [example requests](/eopa/reference/api-reference/preview-api#examples).
