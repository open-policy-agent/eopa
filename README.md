# Styra Load-Private

![OPA v0.50.2](https://openpolicyagent.org/badge/v0.50.2)

## Build

### Prerequisites:

Install using brew or directly from download page.

- [golang](https://go.dev/dl/): `brew install go`
- [golanglint-ci](https://golangci-lint.run/usage/install/): `brew install golanglint-ci`
- [ko-build](https://github.com/ko-build/ko): `brew install ko`
- [Docker](https://docs.docker.com/desktop/install/mac-install/)
- Make: `xcode-select --install`
- [goreleaser](https://goreleaser.com): `brew install goreleaser`
- [protobuf](https://developers.google.com/protocol-buffers): see `pkg/grpc/README.md`
- [bufbuild](https://buf.build/)
- [grpcurl](https://github.com/fullstorydev/grpcurl): `brew install grpcurl`
- [quill](https://github.com/anchore/quill): `curl -sSfL https://raw.githubusercontent.com/anchore/quill/main/install.sh | sh -s -- -b /usr/local/bin`


### Optional:
- [goreleaser-cross](https://github.com/goreleaser/goreleaser-cross): `make release` (1.5GB)
- [visual studio code](https://code.visualstudio.com/download)
- [delve](https://github.com/go-delve/delve/blob/master/Documentation/installation/osx/install.md): `brew install delve`

Build with `make build`, run with `make run`, publish with `make push`.

## Directories

- `bin`: built binaries
- `build`: additional build scripts
- `cmd`: cobra command CLI
- `e2e`: end-to-end tests
- `pkg`: load source
- `proto`: protobuf sources
- `test`: smoke tests data

## Files

- `Makefile`: top-level make
- `main.go`: golang main
- `go.mod`, `go.sum`: golang module configuration: 'make update'
- `.goreleaser.yaml`, `.goreleaser-wasm.yaml`: goreleaser build scripts
- `.golangci.yaml`, `.golangci-optional.yml`: golang lint configuration
- `.github/workflows`: github actions
- `.ko.yaml`: ko-build

## Common make targets

- `make`: build load
- `make fmt`: go fmt
- `make update`: update module configuration
- `make test`: run unittests
- `make check`: run linter

## FAQ

### How can I update the `load` branch of the github.com/StyraInc/opa fork?

- `make update`

### How do I update OPA in Load?

Let's assume an update from OPA 0.49.0 to 0.50.0:

First, we update the fork:

1. push `main` from `github.com/open-policy-agent/opa` to the fork `github.com/StyraInc/opa`
2. push the latest version tag (v0.50.0) from `github.com/open-policy-agent/opa` to the fork (NB: the post-tag action on the fork always fails)
3. checkout the previous fork branch, e.g. `load-0.49`
4. `git rebase v0.50.0` -- rebase on top of the latest release tag
5. name the branch `load-0.50` and push it to the fork `github.com/StyraInc/opa`

Then we update the reference in Load:

1. Update it in `go.mod`: `GOPRIVATE=github.com/StyraInc go get github.com/open-policy-agent/opa@v0.50.0` (NB: this has no consequences except for version-tag bookkeeping)
2. Update `load-xx` in the `update` target of the Makefile
3. Run `make update`.
4. Bump the OPA version number in the `README.md` badge at the top
5. Commit the changes and push a PR to `github.com/StyraInc/load-private`.

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

### Run 'load' documentation locally

From fetchdb repo; see \<fetchdb\>/docs/public/docs-website/README.md

```
brew install npm
cd <fetchdb>/docs/public/docs-website
npm install
npm run start
```
from browser: http://localhost:3000/load

### Permission denied when running 'load'

If you get "permission denied: ./load"

```
% chmod +x load
```

### MacOS 'cannot verify the developer of "load"' after downloading

```
% xattr -d com.apple.quarantine load
```

### MacOS signing locally (`make release`)

Follow the instruction to create an Apple developer certificate (P12) and notary on the [Quill README.md](https://github.com/anchore/quill).

Set up the following environment variables, and perform a `make release`:

```
      QUILL_SIGN_P12: ${{ secrets.QUILL_SIGN_P12 }} # base64 encoded contents
      QUILL_SIGN_PASSWORD: ${{ secrets.QUILL_SIGN_PASSWORD }} # p12 password
      QUILL_NOTARY_KEY: ${{ secrets.QUILL_NOTARY_KEY }}
      QUILL_NOTARY_KEY_ID: ${{ secrets.QUILL_NOTARY_KEY_ID }}
      QUILL_NOTARY_ISSUER: ${{ secrets.QUILL_NOTARY_ISSUER }}
```

### MacOS sign-and-notarize failure

You can safely ignore the error, or set up Quill as described above.

```
  ⨯ release failed after 5s error=post hook failed: failed to run 'quill sign-and-notarize /Users/kevin/src/github.com/styrainc/load-private/dist/darwin-build_darwin_amd64_v1/load -vv': exit status 1
make: *** [release] Error 1
```

## Release Load

Setting the tag version will trigger the .github/workflows/push-tags.yaml action; which will publish 'load' release and 'load' containers to https://github.com/StyraInc/load

### Current version

```
# check the current tag/release
git tag -l --sort -version:refname | head -n 1
```

### Update CHANGELOG.md

```
# Edit the CHANGELOG.md
git commit
git push
```

### Update capabilities

```
# create capabilities (tag+1) and submit capabilities
build/gen-release-patch.sh --version=0.100.1
# create PR and submit generated file: capabiles/v0.100.1.json
git add capabilities/v0.100.1.json
git commit
git push
```

### Tag main and trigger push-tag.yaml action

Final step.

```
# always on main!
git checkout main
# make sure our copy of `main` is up-to-date
git pull
# create tag +1
git tag v0.100.1
# push
git push origin v0.100.1
```

