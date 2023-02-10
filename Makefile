export GOPRIVATE=github.com/StyraInc/opa

ifdef AUTH_RELEASE
NEWEST := $(shell git tag -l --sort -version:refname | head -n 1)
ifeq ($(AUTH_RELEASE), $(NEWEST))
LATEST := --tags latest
LATEST_DEBUG := --tags latest-debug
endif
# only auth-build-ci tag builds get put into 'load' packages repository
export KO_DOCKER_REPO=ghcr.io/styrainc/load
else
export KO_DOCKER_REPO=ghcr.io/styrainc/load-private
endif

GOVERSION ?= $(shell cat ./.go-version)
GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

# default KO_DEFAULTBASEIMAGE = cgr.dev/chainguard/static
KO_DEBUG_IMAGE ?= cgr.dev/chainguard/busybox:latest

KO_BUILD := ko build . --sbom=none --image-label org.opencontainers.image.source=https://github.com/StyraInc/load
KO_BUILD_LOCAL := $(KO_BUILD) --base-import-paths --local
KO_BUILD_DEPLOY := $(KO_BUILD) --bare --platform=linux/amd64,linux/arm64

BUILD_DIR := $(shell echo `pwd`)
RELEASE_DIR := _release

# LOAD_VERSION: strip 'v' from tag
LOAD_VERSION := $(shell git describe --abbrev=0 --tags | sed s/^v//)
VERSION := $(LOAD_VERSION)$(shell ./build/get-plugin-rev.sh)
HOSTNAME ?= $(shell hostname -f)

LOAD_LDFLAGS := -X=github.com/open-policy-agent/opa/version.Program=Load
VERSION_LDFLAGS := -X=github.com/open-policy-agent/opa/version.Version=$(LOAD_VERSION)
TELEMETRY_LDFLAGS := -X=github.com/open-policy-agent/opa/internal/report.ExternalServiceURL=https://telemetry.openpolicyagent.org
HOSTNAME_LDFLAGS := -X=github.com/open-policy-agent/opa/version.Hostname=$(HOSTNAME)

LDFLAGS := $(LOAD_LDFLAGS) $(VERSION_LDFLAGS) $(TELEMETRY_LDFLAGS) $(HOSTNAME_LDFLAGS)

.PHONY: load
load:
	go build -o $(BUILD_DIR)/bin/load "-ldflags=$(LDFLAGS)"

# ko build is used by the GHA workflow to build an container image that can be tested on GHA,
# i.e. linux/amd64 only.
.PHONY: build build-local run build-local-debug push deploy-ci deploy-ci-debug auth-deploy-ci auth-deploy-ci-debug

# build container image file: local.tar
build:
	LOAD_VERSION=$(LOAD_VERSION) $(KO_BUILD) --push=false --tarball=local.tar

# build and run local ko-build container (no tags)
run: build-local
	docker run -e STYRA_LOAD_LICENSE_TOKEN -e STYRA_LOAD_LICENSE_KEY -p 8181:8181 -v $$(pwd):/cwd -w /cwd ko.local/load-private:edge run --server --log-level debug

# build local container image (tagged)
build-local:
	LOAD_VERSION=$(LOAD_VERSION) $(KO_BUILD_LOCAL) --tags $(VERSION) --tags edge

# build container.
# execute: docker run -it --rm --entrypoint sh ko.local/load-private:edge-debug
build-local-debug:
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_IMAGE) LOAD_VERSION=$(LOAD_VERSION) $(KO_BUILD_LOCAL) --disable-optimizations --tags $(VERSION)-debug --tags edge-debug

deploy-ci: push
push:
	LOAD_VERSION=$(LOAD_VERSION) $(KO_BUILD_DEPLOY) --tags $(VERSION) --tags edge

deploy-ci-debug:
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_IMAGE) LOAD_VERSION=$(LOAD_VERSION) $(KO_BUILD_DEPLOY) --disable-optimizations --tags $(VERSION)-debug --tags edge-debug

auth-deploy-ci:
	LOAD_VERSION=$(LOAD_VERSION) $(KO_BUILD_DEPLOY) --tags $(LOAD_VERSION) $(LATEST)

auth-deploy-ci-debug:
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_IMAGE) LOAD_VERSION=$(LOAD_VERSION) $(KO_BUILD_DEPLOY) --disable-optimizations --tags $(LOAD_VERSION)-debug $(LATEST_DEBUG)

# goreleaser uses latest version tag.
.PHONY: release release-ci release-wasm
release:
	HOSTNAME=$(HOSTNAME) LOAD_VERSION=$(LOAD_VERSION) goreleaser release --snapshot --skip-publish --clean

release-ci:
	HOSTNAME=$(HOSTNAME) LOAD_VERSION=$(LOAD_VERSION) goreleaser release --clean

# load docker image ghcr.io/goreleaser/goreleaser-cross:v1.19 and run goreleaser (build load and load_wasm)
release-wasm:
	go mod vendor
	docker run --rm -v $$(PWD):/cwd -w /cwd ghcr.io/goreleaser/goreleaser-cross:v1.19 release -f .goreleaser-wasm.yaml --snapshot --skip-publish --rm-dist

# utilities
.PHONY: test e2e benchmark fmt check update
test:
	go test ./...

e2e:
	go test -tags e2e ./e2e/... -v -count=1 # always run

benchmark:
	go test -run=- -bench=. -benchmem ./...

fmt:
	golangci-lint run -v --fix

check:
	golangci-lint run -v

update:
	go mod edit -replace github.com/open-policy-agent/opa=github.com/StyraInc/opa@load-0.49
	go mod tidy

# ci-smoke-test
#    called by github action
#    run locally:
#      make release
#      make ci-smoke-test ARCHIVE=dist/load_Darwin_x86_64.tar.gz BINARY=load
#
.PHONY: ci-smoke-test
ci-smoke-test:
	mkdir -p $(RELEASE_DIR)
	test -f "$(ARCHIVE)"
ifeq ($(GOOS),windows)
	cd $(RELEASE_DIR); unzip ../$(ARCHIVE)
else
	cd $(RELEASE_DIR); tar xzf ../$(ARCHIVE)
endif
	test -f "$(RELEASE_DIR)/$(BINARY)"
	./build/binary-smoke-test.sh "$(RELEASE_DIR)/$(BINARY)" rego
	rm -rf $(RELEASE_DIR)
