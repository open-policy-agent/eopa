---
title: How to install EOPA as a container
sidebar_label: Container
sidebar_position: 1
---

# Install as a container

Container images are published in GitHub Container Registry and tagged for each release. Image tags correspond to the release version.
See the GitHub [package](https://github.com/open-policy-agent/eopa/pkgs/container/enterprise-opa) for the latest images.

```shell
# terminal-command
docker pull ghcr.io/open-policy-agent/eopa:VERSION
```

If you are testing out EOPA in a non production environment, you might find the `latest` tag convenient:

```shell
# terminal-command
docker pull ghcr.io/open-policy-agent/eopa:latest
```

:::note
You can replicate these images to a registry nearer your cluster to reduce cold start times. This is especially important when:

- Running in an environment without access to pull images from GitHub Container Registry.
- Not using a long-running deployment of EOPA (for example, `eopa eval` in jobs).
- EOPA instance startup time is critical.
:::

To run EOPA in a Kubernetes cluster, please see [deployment documentation](/eopa/how-to/install/kubernetes).
