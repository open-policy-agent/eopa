export GOPRIVATE=github.com/StyraInc/opa

ifdef AUTH_RELEASE
NEWEST := $(shell git tag -l --sort -version:refname | head -n 1)
ifeq ($(AUTH_RELEASE), $(NEWEST))
LATEST := --tags latest
LATEST_DEBUG := --tags latest-debug
endif
# only auth-build-ci tag builds get put into 'enterprise-opa' packages repository
export KO_DOCKER_REPO=ghcr.io/styrainc/enterprise-opa
else
export KO_DOCKER_REPO=ghcr.io/styrainc/enterprise-opa-private
endif

GOVERSION ?= $(shell cat ./.go-version)
GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

# default KO_DEFAULTBASEIMAGE = cgr.dev/chainguard/static
KO_DEBUG_IMAGE ?= cgr.dev/chainguard/busybox:latest

KO_BUILD := ko build . --image-label org.opencontainers.image.source=https://github.com/StyraInc/enterprise-opa
KO_BUILD_LOCAL := KO_DOCKER_REPO=ko.local $(KO_BUILD) --base-import-paths
KO_BUILD_DEPLOY := $(KO_BUILD) --bare --platform=linux/amd64,linux/arm64

BUILD_DIR := $(shell echo `pwd`)
FUZZ_TIME ?= 30m

# EOPA_VERSION: strip 'v' from tag
export EOPA_VERSION := $(shell git describe --abbrev=0 --tags | sed s/^v//)
VERSION := $(EOPA_VERSION)$(shell ./build/get-plugin-rev.sh)
export OPA_VERSION := $(shell ./build/get-opa-version.sh)
HOSTNAME ?= $(shell hostname -f)

EOPA_LDFLAGS := "-X=github.com/open-policy-agent/opa/version.Program=Enterprise OPA"
ALT_EOPA_LDFLAGS := "-X=github.com/open-policy-agent/opa/version.AltProgram=Open Policy Agent"
VERSION_LDFLAGS := -X=github.com/open-policy-agent/opa/version.Version=$(EOPA_VERSION)
ALT_VERSION_LDFLAGS := -X=github.com/open-policy-agent/opa/version.AltVersion=$(OPA_VERSION)
# TODO: Update this URL when a new address for the telemetry service is available
TELEMETRY_LDFLAGS := -X=github.com/open-policy-agent/opa/internal/report.ExternalServiceURL=https://load-telemetry.corp.styra.com
HOSTNAME_LDFLAGS := -X=github.com/open-policy-agent/opa/version.Hostname=$(HOSTNAME)

LDFLAGS := $(VERSION_LDFLAGS) $(EOPA_LDFLAGS) $(ALT_EOPA_LDFLAGS) $(ALT_VERSION_LDFLAGS) $(TELEMETRY_LDFLAGS) $(HOSTNAME_LDFLAGS)

.PHONY: eopa
eopa:
	go build -o $(BUILD_DIR)/bin/eopa '-ldflags=$(LDFLAGS)'

# ko build is used by the GHA workflow to build an container image that can be tested on GHA,
# i.e. linux/amd64 only.
.PHONY: build build-local run build-local-debug push deploy-ci deploy-ci-debug auth-deploy-ci auth-deploy-ci-debug

# build container image file: local.tar
build:
	$(KO_BUILD) --push=false --tarball=local.tar

# build and run local ko-build container (no tags)
run: build-local
	docker run -e EOPA_LICENSE_TOKEN -e EOPA_LICENSE_KEY -p 8181:8181 -v $$(pwd):/cwd -w /cwd ko.local/enterprise-opa-private:edge run --server --log-level debug

# build local container image (tagged)
build-local:
	$(KO_BUILD_LOCAL) --tags $(VERSION) --tags edge

# build container.
# execute: docker run -it --rm --entrypoint sh ko.local/enterprise-opa-private:edge-debug
build-local-debug:
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_IMAGE) $(KO_BUILD_LOCAL) --disable-optimizations --tags $(VERSION)-debug --tags edge-debug

deploy-ci: push
push:
	$(KO_BUILD_DEPLOY) --tags $(VERSION) --tags edge

deploy-ci-debug:
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_IMAGE) $(KO_BUILD_DEPLOY) --disable-optimizations --tags $(VERSION)-debug --tags edge-debug

auth-deploy-ci:
	$(KO_BUILD_DEPLOY) --tags $(EOPA_VERSION) $(LATEST)

auth-deploy-ci-debug:
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_IMAGE) $(KO_BUILD_DEPLOY) --disable-optimizations --tags $(EOPA_VERSION)-debug $(LATEST_DEBUG)

# goreleaser uses latest version tag.
.PHONY: release release-ci release-wasm release-single
release:
	HOSTNAME=$(HOSTNAME) goreleaser release --snapshot --skip-publish --clean

release-single:
	HOSTNAME=$(HOSTNAME) goreleaser build --snapshot --clean --single-target --id linux-windows-build

release-ci:
	HOSTNAME=$(HOSTNAME) goreleaser release --clean --release-notes CHANGELOG.md

# enterprise OPA docker image ghcr.io/goreleaser/goreleaser-cross:v1.19 and run goreleaser (build eopa and eopa_wasm)
release-wasm:
	go mod vendor
	docker run --rm -v $$(PWD):/cwd -w /cwd ghcr.io/goreleaser/goreleaser-cross:v1.19 release -f .goreleaser-wasm.yaml --snapshot --skip-publish --rm-dist

# utilities
.PHONY: test test-race e2e benchmark fmt check fuzz update
test:
	go test ./...

test-race:
	go test ./... -race

e2e:
	go test -p 1 -tags e2e ./e2e/... -v -count=1 # always run, no parallelism

benchmark:
	go test -run=- -bench=. -benchmem ./...

fmt:
	golangci-lint run -v --fix

check:
	golangci-lint run -v

fuzz:
	go test ./pkg/json -fuzz FuzzDecode -fuzztime ${FUZZ_TIME} -v -run '^$$'

update:
	go mod edit -replace github.com/open-policy-agent/opa=github.com/StyraInc/opa@eopa-0.53.1
	go mod tidy

# ci-smoke-test
#    called by github action
#    run locally:
#      make release
#      make ci-smoke-test BINARY=dist/darwin-build_darwin_amd64_v1/eopa
#
.PHONY: ci-smoke-test
ci-smoke-test:
	test -f "$(BINARY)"
	chmod +x "$(BINARY)"
	./build/binary-smoke-test.sh "$(BINARY)" rego

generate-cli-docs:
	rm -rf tmp-docs
	mkdir tmp-docs
	go run build/generate-cli-docs/generate.go tmp-docs
