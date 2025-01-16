# Styra Enterprise OPA Private

![OPA v1.0.0](https://openpolicyagent.org/badge/v1.0.0)
[![Regal v0.30.0](https://img.shields.io/github/v/release/styrainc/regal?filter=v0.30.0&label=Regal)](https://github.com/StyraInc/regal/releases/tag/v0.30.0)

## Github Source and Project

- [Enterprise OPA Dev Board](https://github.com/orgs/StyraInc/projects/4/views/1)
- [enterprise-opa-private](https://github.com/StyraInc/enterprise-opa-private)
- [opa](https://github.com/StyraInc/opa)
- [enterprise-opa](https://github.com/StyraInc/enterprise-opa)

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

## Supporting services

While this repository tracks the code required to build Styra Enterprise OPA, some additional services have been built to help with the free trial, telemetry, and other "supporting" tasks.

### Free Trial services

 - `fetchdb` service [[source](https://github.com/StyraInc/fetchdb/tree/main/services/eopa-trial-generator)] :: `eopa-trial-generator`: Generates licenses for each free trial signup.
   - `kubectl` context: `kubectl config use-context eopa-trial-generator-prod`
 - `fetchdb` service [[source](https://github.com/StyraInc/fetchdb/tree/main/services/eopa-trial-activation-tracker)] :: `eopa-trial-activation-tracker`: Pushes first-time license activation events to Marketo.
   - `kubectl` context: `kubectl config use-context eopa-trial-generator-prod` (Same group as the `-generator` service.)
 - Concourse job [[source](https://github.com/StyraInc/concourse-defs/tree/main/eopa/licenses-reaper)] :: `eopa/licenses-reaper`: Weekly job to remove expired trial licenses from Keygen.sh.

### Telemetry services

 - `fetchdb` service [[source](https://github.com/StyraInc/fetchdb/tree/main/services/eopa-telemetry)] :: `eopa-telemetry`: Enterpriser OPA version of the "OPA Telemetry" service.
   - `kubectl` context: `kubectl config use-context eopa-telemetry-prod`
 - `fetchdb` service [[source](https://github.com/StyraInc/fetchdb/tree/main/services/eopa-telemetry-dashboard)] :: `eopa-telemetry-dashboard`: Enterprise OPA version of the "OPA Telemetry Dashboard" service.
   - `kubectl` context: `kubectl config use-context eopa-telemetry-prod`
   - Dashboard URL: https://ops-prod.k8s.styra.com/v1/service/eopa-telemetry-dashboard.eopa-telemetry:8080/

### Monitoring

 - NewRelic Dashboard [[Link](https://one.newrelic.com/dashboards/detail/MzU5NDA4OHxWSVp8REFTSEJPQVJEfGRhOjMxNTUwNjQ?account=3594088&state=0e224a3f-d3cc-492f-a8c6-fb2fd1caa0e0)]: `Enterprise OPA Health`: Tracks the key metrics / status of all `eopa-trial-*` and `eopa-telemetry*` services.
 - Terraform Rules for Alerts [[Link](https://github.com/StyraInc/platform-terraform/blob/main/newrelic/alerts/load_alerts.tf)]: Any monitoring config changes made through the NewRelic UI will be automatically overwritten with whatever Terraform is told to provision. To make lasting changes to alerting, change them in the Terraform configs.

## FAQ

### How can I update the `eopa` branch of the github.com/StyraInc/opa fork?

- `make update && make update-e2e && make update-examples`

### How do I update OPA in Enterprise OPA?

Let's assume an update from OPA 0.49.0 to 0.50.0:

First, we update the fork:

1. push `main` from `github.com/open-policy-agent/opa` to the fork `github.com/StyraInc/opa`
2. push the latest version tag (v0.50.0) from `github.com/open-policy-agent/opa` to the fork (NB: the post-tag action on the fork always fails)
3. checkout the previous fork branch, e.g. `eopa-0.49`
4. `git rebase v0.50.0` -- rebase on top of the latest release tag
5. name the branch `eopa-0.50` and push it to the fork `github.com/StyraInc/opa`

Then we update the reference in Enterprise OPA:

1. Update it in `go.mod`: `GOPRIVATE=github.com/StyraInc go get github.com/open-policy-agent/opa@v0.50.0` (NB: this has no consequences except for version-tag bookkeeping)
   - You will need to do this step for the `e2e/` folder as well, since it's dependencies are managed as a separate Go workspace.
   - This step should also be repeated for each subfolder under `examples/`, along with a `go mod tidy` run for each example.
2. Update `eopa-xx` in the `update` target of the Makefile
3. Run `make update && make update-e2e && make update-examples`.
4. Bump the OPA version number in the `README.md` badge at the top
5. Commit the changes and push a PR to `github.com/StyraInc/enterprise-opa-private`.

### Can't build locally: private github repo

````
go: errors parsing go.mod:
/Users/stephan/Sources/StyraInc/enterprise-opa/go.mod:89: replace github.com/StyraInc/opa: version "eopa" invalid: git ls-remote -q origin in /Users/stephan/go/pkg/mod/cache/vcs/39c7f8258aa43a0e71284d9afa9390ab62dcf0466b0baf3bc3feef290c1fe63d: exit status 128:
	fatal: could not read Username for 'https://github.com': terminal prompts disabled
Confirm the import path was entered correctly.
If this is a private repository, see https://golang.org/doc/faq#git_https for additional information.
````

Adding this snippet to your .gitconfig should help:
```
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
```

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

### MacOS sign-and-notarize failure

You can safely ignore the error, or set up Quill as described above.

```
  ⨯ release failed after 5s error=post hook failed: failed to run 'quill sign-and-notarize /Users/kevin/src/github.com/styrainc/enterprise-opa-private/dist/darwin-build_darwin_amd64_v1/eopa -vv': exit status 1
make: *** [release] Error 1
```

## Release Enterprise OPA

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
