---
title: How to install Enterprise OPA binaries locally
sidebar_label: Local binaries
sidebar_position: 0
---

# Install binaries


## macOS and Linux

On macOS and Linux, `brew` is the preferred installation method, as updates to Enterprise OPA will be handled automatically.

```shell
# terminal-command
brew install styrainc/packages/eopa
```


## Direct Downloads

- [macOS (Apple Silicon)](https://github.com/StyraInc/enterprise-opa/releases/latest/download/eopa_Darwin_arm64)
- [macOS (x86_64)](https://github.com/StyraInc/enterprise-opa/releases/latest/download/eopa_Darwin_x86_64)
- [Linux (x86_64)](https://github.com/StyraInc/enterprise-opa/releases/latest/download/eopa_Linux_x86_64)
- [Linux (arm64)](https://github.com/StyraInc/enterprise-opa/releases/latest/download/eopa_Linux_arm64)
- [Windows (x86_64)](https://github.com/StyraInc/enterprise-opa/releases/latest/download/eopa_Windows_x86_64.exe)
- [Checksums](https://github.com/StyraInc/enterprise-opa/releases/latest/download/checksums.txt)


**Windows and macOS** binaries are provided for convenience and exploration. Only Linux binaries and container images are supported for use in production environments.

Once you have downloaded and installed an Enterprise OPA binary in your `$PATH`, you can verify the installation by running the following command:

```shell
# terminal-command
eopa version
```
