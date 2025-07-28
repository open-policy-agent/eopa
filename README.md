# EOPA

![OPA v1.4.0](https://openpolicyagent.org/badge/v1.4.0)
[![Regal v0.33.1](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.33.1&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.33.1)

## Build

### Prerequisites:

Install using brew or directly from download page.

- [golang](https://go.dev/dl/): `brew install go`
- [golanglint-ci](https://golangci-lint.run/usage/install/): `brew install golangci-lint`
- [ko-build](https://github.com/ko-build/ko): `brew install ko`
- [skopeo](https://github.com/containers/skopeo/tree/main): `brew install skopeo`
- [apko](https://github.com/chainguard-dev/apko): `brew install apko`
- [Docker](https://docs.docker.com/desktop/install/mac-install/) (or OrbStack)
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
- [vault](https://developer.hashicorp.com/vault/downloads)

Build with `make build`, run with `make run`, publish with `make push`.

## Directories

- `bin`: built binaries
- `build`: additional build scripts
- `cmd`: cobra command CLI
- `e2e`: end-to-end tests
- `pkg`: enterprise OPA source
- `proto`: protobuf sources
- `test`: smoke tests data

## Files

- `Makefile`: top-level make
- `main.go`: golang main
- `go.mod`, `go.sum`: golang module configuration: 'make update'
- `.goreleaser.yaml`: goreleaser build scripts
- `.golangci.yaml`: golang lint configuration
- `.github/workflows`: github actions
- `.ko.yaml`: ko-build

## Common make targets

- `make`: build eopa
- `make fmt`: go fmt
- `make update`/`make update-e2e`/`make update-examples`: update module configuration
- `make test`: run unittests
- `make check`: run linter

## FAQ

### Run 'eopa' documentation locally

From fetchdb repo; see \<fetchdb\>/docs/public/docs-website/README.md

```
brew install npm
cd <fetchdb>/docs/public/docs-website
npm install
npm run start
```
from browser: http://localhost:3000/enterprise-opa

### Generate/Update CLI documentation

Run the following command to regenerate the CLI documentation.
Apply diff manually to fetchdb

```
make generate-cli-docs
diff tmp-docs/cli.md ../fetchdb/docs/public/docs/enterprise-opa/cli-reference.md
```

### Permission denied when running 'eopa'

If you get "permission denied: ./eopa"

```
% chmod +x eopa
```

### MacOS 'cannot verify the developer of "eopa"' after downloading

```
% xattr -d com.apple.quarantine eopa
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

### MacOS sign-and-notarize failure for local builds

You can safely ignore the error, or set up Quill as described above.

```
  ⨯ release failed after 5s error=post hook failed: failed to run 'quill sign-and-notarize /Users/kevin/src/github.com/open-policy-agent/eopa/dist/darwin-build_darwin_amd64_v1/eopa -vv': exit status 1
make: *** [release] Error 1
```

### Release pipeline fails in notarization step

We have seen two different causes of failure so far for Quill signing and notarization of the binaries in CI:
 - Our company Apple Developer account needs to accept a new agreement.
   - Resolution: Ask Stephan or ops to check for a new agreement. If there was a new agreement, then re-run the job after accepting. (Links: [page with quill keys](https://appstoreconnect.apple.com/access/integrations/api), [Account overview](https://developer.apple.com/account))
 - The Apple notarization service itself is down.
   - Resolution: Check the Apple Developer [System Status](https://developer.apple.com/system-status/) page for outages. If there's an outage, just wait until the service comes back up, and then re-run the job.

## Release EOPA

Setting the tag version will trigger the .github/workflows/push-tags.yaml action; which will publish 'eopa' release and 'enterprise-opa' containers to https://github.com/StyraInc/enterprise-opa

### Current version

```
# check the current tag/release
git fetch
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
