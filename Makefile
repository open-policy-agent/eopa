export GOPRIVATE=github.com/StyraInc/opa
export KO_DOCKER_REPO=547414210802.dkr.ecr.us-east-1.amazonaws.com/styra

VERSION_OPA := $(shell ./build/get-opa-version.sh)
VERSION := $(VERSION_OPA)$(shell ./build/get-plugin-rev.sh)
ECR_PASSWORD := $(shell aws ecr get-login-password --region us-east-1)

KO_BUILD := ko build --sbom=none --base-import-paths --platform=linux/amd64 --tags $(VERSION)

BUILD_DIR := $(shell echo `pwd`)

.PHONY: load build build-local push test fmt check run update docker-login deploy-ci

load:
	go build -o $(BUILD_DIR)/bin/load .

build:
	$(KO_BUILD) --push=false --tarball=local.tar

build-local:
	@$(KO_BUILD) --local --tags edge

push:
	$(KO_BUILD) --tags edge

test:
	go test ./...

fmt:
	golangci-lint run -v --fix

check:
	golangci-lint run -v

run:
	docker run -p 8181:8181 -v $$(pwd):/cwd -w /cwd $$($(KO_BUILD) --local) run --server --log-level debug

update:
	go mod edit -replace github.com/open-policy-agent/opa=github.com/StyraInc/opa@load-0.47
	go mod tidy

docker-login:
	@echo "Docker Login..."
	@echo ${ECR_PASSWORD} | docker login --username AWS --password-stdin 547414210802.dkr.ecr.us-east-1.amazonaws.com

deploy-ci: docker-login push
