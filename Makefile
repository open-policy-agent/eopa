export GOPRIVATE=github.com/StyraInc/opa
export KO_DOCKER_REPO=547414210802.dkr.ecr.us-east-1.amazonaws.com/styra

VERSION_OPA := $(shell ./build/get-opa-version.sh)
VERSION := $(VERSION_OPA)$(shell ./build/get-plugin-rev.sh)

GOVERSION ?= $(shell cat ./.go-version)
GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

KO_BUILD := ko build --sbom=none --base-import-paths --tags $(VERSION)
KO_BUILD_ALL := $(KO_BUILD) --platform=linux/amd64,linux/arm64

BUILD_DIR := $(shell echo `pwd`)
RELEASE_DIR := _release

.PHONY: load build release release-wasm build-local push test fmt check run update docker-login deploy-ci e2e

load:
	go build -o $(BUILD_DIR)/bin/load .

# build is used by the GHA workflow to build an image that can be tested on GHA,
# i.e. linux/amd64 only.
build:
	$(KO_BUILD) --push=false --tarball=local.tar

# goreleaser uses latest version tag.
release:
	goreleaser release --snapshot --skip-publish --rm-dist

# load docker image ghcr.io/goreleaser/goreleaser-cross:v1.19 and run goreleaser (build load and load_wasm)
release-wasm:
	go mod vendor
	docker run --rm -v $$(PWD):/cwd -w /cwd ghcr.io/goreleaser/goreleaser-cross:v1.19 release -f .goreleaser-wasm.yaml --snapshot --skip-publish --rm-dist

build-local:
	@$(KO_BUILD_ALL) --local --tags edge

push:
	$(KO_BUILD_ALL) --tags edge

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

run:
	docker run -e STYRA_LOAD_LICENSE_TOKEN -e STYRA_LOAD_LICENSE_KEY -p 8181:8181 -v $$(pwd):/cwd -w /cwd $$($(KO_BUILD) --local) run --server --log-level debug

update:
	go mod edit -replace github.com/open-policy-agent/opa=github.com/StyraInc/opa@load-0.48
	go mod tidy

docker-login:
	@echo "Docker Login..."
	@aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin 547414210802.dkr.ecr.us-east-1.amazonaws.com

deploy-ci: docker-login push

# ci-smoke-test
#    called by github action
#    run locally:
#      make release
#      make ci-smoke-test ARCHIVE=dist/load_Darwin_all.tar.gz BINARY=load
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
