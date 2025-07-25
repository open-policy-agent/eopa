BUILD_ARGS :=

ifdef AUTH_RELEASE
NEWEST := $(shell git tag -l --sort -version:refname | head -n 1)
ifeq ($(AUTH_RELEASE), $(NEWEST))
LATEST := --tags latest
LATEST_DEBUG := --tags latest-debug
endif
endif

GOVERSION ?= $(shell cat ./.go-version)
GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

ifeq ($(shell uname -m), x86_64)
	ARCH := amd64
else
	ARCH := arm64
endif

KO_DEBUG_BASE_IMAGE_LOCAL := ko.local/kobase:debug
KO_BASE_IMAGE_LOCAL := ko.local/kobase:base
KO_DEBUG_BASE_IMAGE := ghcr.io/open-policy-agent/eopa-debug:latest
KO_BASE_IMAGE := ghcr.io/open-policy-agent/eopa-base:latest

ifeq ($(SKIP_IMAGES), true)
	KO_DEBUG_BASE_IMAGE_LOCAL_REF := $(KO_DEBUG_BASE_IMAGE)
	KO_BASE_IMAGE_LOCAL_REF := $(KO_BASE_IMAGE)
else
	KO_DEBUG_BASE_IMAGE_LOCAL_REF := $(KO_DEBUG_BASE_IMAGE_LOCAL)-$(ARCH)
	KO_BASE_IMAGE_LOCAL_REF := $(KO_BASE_IMAGE_LOCAL)-$(ARCH)
endif

# all images are pushed into the public repo
# only release images are tagged "latest"
export KO_DOCKER_REPO=ghcr.io/open-policy-agent/enterprise-opa
KO_BUILD := ko build . --image-label org.opencontainers.image.source=https://github.com/open-policy-agent/eopa
KO_BUILD_DEPLOY := $(KO_BUILD) --bare --platform=linux/amd64,linux/arm64

BUILD_DIR := $(shell echo `pwd`)
FUZZ_TIME ?= 30m
RELEASE_NOTES ?= "release-notes.md"
EXAMPLES := $(wildcard examples/*)

# EOPA_VERSION: strip 'v' from tag
# TODO(sr): Go back to this when we go back to pushing tags: $(shell git describe --abbrev=0 --tags | sed s/^v//)
export EOPA_VERSION := 0.0.1
VERSION := $(EOPA_VERSION)$(shell ./build/get-plugin-rev.sh)
export OPA_VERSION := $(shell ./build/get-opa-version.sh)
HOSTNAME ?= $(shell hostname -f)

VERSION_LDFLAGS := -X=github.com/open-policy-agent/eopa/internal/version.Version=$(EOPA_VERSION)
TELEMETRY_LDFLAGS := -X=github.com/open-policy-agent/opa/internal/report.ExternalServiceURL=$(EOPA_TELEMETRY_URL)
HOSTNAME_LDFLAGS := -X=github.com/open-policy-agent/eopa/internal/version.Hostname=$(HOSTNAME)
# goreleaser reads this via .goreleaser.yaml
export EOPA_TELEMETRY_URL ?= https://load-telemetry.corp.styra.com

LDFLAGS := $(VERSION_LDFLAGS) $(TELEMETRY_LDFLAGS) $(HOSTNAME_LDFLAGS)

.PHONY: eopa
eopa:
	go build $(BUILD_ARGS) -o $(BUILD_DIR)/bin/eopa '-ldflags=$(LDFLAGS)'

# ko build is used by the GHA workflow to build an container image that can be tested on GHA,
# i.e. linux/amd64 only.
.PHONY: build build-local run build-local-debug push deploy-ci deploy-ci-debug auth-deploy-ci auth-deploy-ci-debug

base-image:
ifeq ($(SKIP_IMAGES), true)
	@echo skipping local image build
  else
	apko build --arch=host apko.yaml $(KO_BASE_IMAGE_LOCAL) base.tar
	docker load --quiet --input=base.tar
	rm base.tar
endif

base-image-debug:
ifeq ($(SKIP_IMAGES), true)
	@echo skipping local image build
else
	apko build --arch=host apko-debug.yaml $(KO_DEBUG_BASE_IMAGE_LOCAL) base-debug.tar
	docker load --quiet --input=base-debug.tar
	rm base-debug.tar
endif

# build container image file: local.tar
build: base-image
	KO_DEFAULTBASEIMAGE=$(KO_BASE_IMAGE_LOCAL_REF) $(KO_BUILD) --push=false --tarball=local.tar

build-debug: base-image-debug
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_BASE_IMAGE_LOCAL_REF) $(KO_BUILD) --disable-optimizations --push=false --tarball=local-debug.tar

# build and run local ko-build container (no tags)
run: build-local
	docker run -p 8181:8181 -v $$(pwd):/cwd -w /cwd ko.local/eopa:edge run --server --log-level debug

# build local container image (tagged)
build-local:
	KO_DEFAULTBASEIMAGE=$(KO_BASE_IMAGE_LOCAL_REF) $(KO_BUILD) --push=false --tarball=local.tar --platform "linux/$(ARCH)"
	skopeo copy docker-archive:local.tar docker-daemon:ko.local/eopa:edge

# build container.
# execute: docker run -it --rm --entrypoint sh ko.local/eopa:edge-debug
build-local-debug: build-debug
	skopeo copy docker-archive:local-debug.tar docker-daemon:ko.local/eopa:edge-debug

deploy-ci: push
push: # HERE the base image needs to be multi-arch!
	KO_DEFAULTBASEIMAGE=$(KO_BASE_IMAGE) $(KO_BUILD_DEPLOY) --tags $(VERSION) --tags edge

deploy-ci-debug:
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_BASE_IMAGE) $(KO_BUILD_DEPLOY) --disable-optimizations --tags $(VERSION)-debug --tags edge-debug

auth-deploy-ci:
	KO_DEFAULTBASEIMAGE=$(KO_BASE_IMAGE) $(KO_BUILD_DEPLOY) --tags $(EOPA_VERSION) $(LATEST)

auth-deploy-ci-debug:
	KO_DEFAULTBASEIMAGE=$(KO_DEBUG_BASE_IMAGE) $(KO_BUILD_DEPLOY) --disable-optimizations --tags $(EOPA_VERSION)-debug $(LATEST_DEBUG)

# goreleaser uses latest version tag.
.PHONY: release release-ci release-wasm release-single
release:
	HOSTNAME=$(HOSTNAME) goreleaser release --snapshot --skip=publish --clean --timeout=60m

release-single:
	HOSTNAME=$(HOSTNAME) goreleaser build --snapshot --clean --single-target

release-ci:
	./build/latest-release-notes.sh --output="${RELEASE_NOTES}"
	HOSTNAME=$(HOSTNAME) goreleaser release --clean --release-notes "${RELEASE_NOTES}" --timeout=60m

# enterprise OPA docker image ghcr.io/goreleaser/goreleaser-cross:v1.19 and run goreleaser (build eopa and eopa_wasm)
release-wasm:
	go mod vendor
	docker run --rm -v $$(PWD):/cwd -w /cwd ghcr.io/goreleaser/goreleaser-cross:v1.19 release -f .goreleaser-wasm.yaml --snapshot --skip-publish --rm-dist

# utilities
.PHONY: test test-race e2e benchmark fmt check fuzz update
test:
	GODEBUG=x509negativeserial=1 go test $(BUILD_ARGS) ./...

test-examples-%:
	cd examples/$* && \
	  GOPRIVATE=github.com/open-policy-agent/eopa go mod tidy && \
	  go test -vet=off .

test-race:
	GODEBUG=x509negativeserial=1 go test $(BUILD_ARGS) ./... -race

e2e:
	cd e2e && \
	  go mod tidy && \
	  go test -failfast -p 1 $(BUILD_ARGS) -tags e2e ./... -v -count=1

benchmark:
	go test $(BUILD_ARGS) -run=- -bench=. -benchmem ./pkg/...

fmt:
	golangci-lint run -v --fix

check:
	golangci-lint run -v

fuzz:
	go test $(BUILD_ARGS)  ./pkg/json -fuzz FuzzDecode -fuzztime ${FUZZ_TIME} -v -run '^$$'

update:
	go get github.com/open-policy-agent/opa@main
	go mod tidy

update-e2e:
	cd e2e \
		&& go get github.com/open-policy-agent/opa@main \
		&& go mod tidy

update-examples:
	$(foreach example, $(EXAMPLES), (cd $(example) && go mod tidy);)

.PHONY: ci-smoke-test
ci-smoke-test:
	test -f "$(BINARY)"
	chmod +x "$(BINARY)"
	./build/binary-smoke-test.sh "$(BINARY)" rego

generate-cli-docs:
	rm -rf tmp-docs
	mkdir tmp-docs
	go run $(BUILD_ARGS) build/generate-cli-docs/generate.go tmp-docs
