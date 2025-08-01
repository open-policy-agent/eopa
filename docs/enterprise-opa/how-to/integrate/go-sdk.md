---
sidebar_position: 2
sidebar_label: SDK package in Go
title: How to integrate with EOPA in Go with the SDK package
---

<!-- markdownlint-disable MD044 -->
import InstallGoModule from '../_install_go_module.md'


# How to integrate EOPA in a Go application with the SDK package

This how-to guide shows how to embed EOPA into a Go application that uses OPA's
[SDK](https://pkg.go.dev/github.com/open-policy-agent/opa/sdk) package.


## Add the Go module dependency

<InstallGoModule />


## Import the EOPA SDK package

Import EOPA into the application with the following import and then
override the options that your application provides when instantiating the SDK.

```go
import eopa_sdk "github.com/styrainc/enterprise-opa-private/pkg/sdk"
```

```go
        # diff-add-start
       opts := eopa_sdk.DefaultOptions()
       opts.ID = "eopa-test-1"
       opts.Config = bytes.NewReader(config)
        # diff-add-end
        // create an instance of the OPA object
        # diff-remove-start
       opa, err := sdk.New(ctx, sdk.Options{
               ID:     "opa-test-1",
               Config: bytes.NewReader(config),
       })
        # diff-remove-end
        # diff-add
       opa, err := sdk.New(ctx, opts)
```

> The `DefaultOptions` enables the EOPA VM target globally as a side-effect.

Since EOPA's data source and decision log sink features are
implemented as standard OPA plugins, there is no additional implementation
required. To enable specific sources and sinks edit the configuration passed to
the SDK. See the [Configuration](/enterprise-opa/reference/configuration)
documentation for details.


## Wrap up

This how-to guide showed how you can embed EOPA into a Go application that
uses the [SDK](https://pkg.go.dev/github.com/open-policy-agent/opa/sdk) package.
The sample code is hosted on
[GitHub](https://github.com/StyraInc/enterprise-opa/tree/main/examples/go-sdk).
For additional examples see the `examples/sdk` directory from the tarball.
