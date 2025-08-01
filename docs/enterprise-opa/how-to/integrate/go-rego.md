---
sidebar_position: 1
sidebar_label: Rego package in Go
title: How to integrate with EOPA in Go with the Rego package
---

<!-- markdownlint-disable MD044 -->
import InstallGoModule from '../_install_go_module.md'


# How to integrate EOPA in a Go application with the Rego package

This how-to guide shows how to embed EOPA into a Go application
and enable EOPA's VM target with the
[rego](https://pkg.go.dev/github.com/open-policy-agent/opa/rego) package.


## Add the Go module dependency

<InstallGoModule />


## Import the EOPA VM package

Add the following import to the Go application. You should add this import in
the file(s) in your application that integrates with the
[rego](https://pkg.go.dev/github.com/open-policy-agent/opa/rego) package.

```go
import eopa_vm "github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
```


## Enable the EOPA VM target

Enable EOPA's optimized evaluation engine by passing the
`eopa_vm.Target` target when constructing the `rego.Rego` object.

```go
        // Construct a Rego object that can be prepared or evaluated.
        r := rego.New(
                rego.Query(os.Args[2]),
                # diff-remove
                rego.Load([]string{os.Args[1]}, nil)),
                # diff-add
                rego.Load([]string{os.Args[1]}, nil),
                # diff-add
                rego.Target(eopa_vm.Target),
        )

        // Create a prepared query that can be evaluated.
        query, err := r.PrepareForEval(ctx)
```


## Wrap up

This how-to guide showed how you can embed EOPA into a Go application that
uses the [rego](https://pkg.go.dev/github.com/open-policy-agent/opa/rego)
package. The sample code is hosted on
[GitHub](https://github.com/StyraInc/enterprise-opa/tree/main/examples/go-rego).
For additional examples see the `examples/rego` directory from the tarball.
