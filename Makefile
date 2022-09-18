export GOPRIVATE=github.com/StyraInc/opa
export KO_DOCKER_REPO=547414210802.dkr.ecr.us-east-1.amazonaws.com/styra

VERSION_OPA := $(shell ./build/get-opa-version.sh)
VERSION := $(VERSION_OPA)$(shell ./build/get-plugin-rev.sh)
ECR_PASSWORD := $(shell aws ecr get-login-password --region us-east-1)

KO_BUILD := ko build --sbom=none --base-import-paths --platform=linux/amd64 --tags $(VERSION)

.PHONY: build build-local push run update docker-login deploy-ci

build:
	$(KO_BUILD) --push=false

build-local:
	@$(KO_BUILD) --local

push:
	$(KO_BUILD)

test:
	go test ./...

run:
	docker run -p 8181:8181 -v $$(pwd):/cwd -w /cwd $$($(KO_BUILD) --local) run --config-file config.yml --server --log-level debug

update:
	go mod edit -replace github.com/open-policy-agent/opa=github.com/StyraInc/opa@load
	go mod tidy

docker-login:
	@echo "Docker Login..."
	@echo ${ECR_PASSWORD} | docker login --username AWS --password-stdin 547414210802.dkr.ecr.us-east-1.amazonaws.com

deploy-ci: docker-login push
