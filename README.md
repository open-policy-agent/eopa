# Styra Load-Private

![OPA v0.49.0](https://openpolicyagent.org/badge/v0.49.0)

## Build

### Prerequisites:

Install using brew or directly from download page.

- [golang](https://go.dev/dl/): `brew install go`
- [golanglint-ci](https://golangci-lint.run/usage/install/): `brew install golanglint-ci`
- [ko-build](https://github.com/ko-build/ko): `brew install ko`
- [Docker](https://docs.docker.com/desktop/install/mac-install/)
- Make: `xcode-select --install`
- [goreleaser](https://goreleaser.com): `brew install goreleaser`
- [protobuf](https://developers.google.com/protocol-buffers): see pkg/grpc/README.md
- [bufbuild](https://buf.build/)

### Optional:
- [goreleaser-cross](https://github.com/goreleaser/goreleaser-cross): `make release` (1.5GB)
- [visual studio code](https://code.visualstudio.com/download)
- [delve](https://github.com/go-delve/delve/blob/master/Documentation/installation/osx/install.md): `brew install delve`

Build with `make build`, run with `make run`, publish with `make push`.

## Directories

- bin: built binaries
- build: additional build scripts
- cmd: cobra command CLI
- e2e: end-to-end tests
- pkg: load source
- test: smoke tests data

## Files

- Makefile: toplevel make
- main.go: golang main
- go.mod, go.sum: golang module configuration: 'make update'
- .goreleaser.yaml, .goreleaser-wasm.yaml: goreleaser build scripts
- .golangci.yaml, .golangci-optional.yml: golang lint configuration
- .github/workflows: github actions
- .ko.yaml: ko-build

## Common make targets

- make: build load
- make fmt: go fmt
- make update: update module configuration
- make test: run unittests
- make check: run linter

## FAQ

### How can I update the `load` branch of the github.com/StyraInc/opa fork?

- `make update`

### Can't build locally: private github repo

````
go: errors parsing go.mod:
/Users/stephan/Sources/StyraInc/load/go.mod:89: replace github.com/StyraInc/opa: version "load" invalid: git ls-remote -q origin in /Users/stephan/go/pkg/mod/cache/vcs/39c7f8258aa43a0e71284d9afa9390ab62dcf0466b0baf3bc3feef290c1fe63d: exit status 128:
	fatal: could not read Username for 'https://github.com': terminal prompts disabled
Confirm the import path was entered correctly.
If this is a private repository, see https://golang.org/doc/faq#git_https for additional information.
````

Adding this snippet to your .gitconfig should help:
```
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
```
