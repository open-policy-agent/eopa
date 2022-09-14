GOPRIVATE=github.com/StyraInc/opa

VERSION_OPA := $(shell ./build/get-opa-version.sh)
VERSION := $(VERSION_OPA)$(shell ./build/get-plugin-rev.sh)
ECR_USER := $(shell aws sts get-caller-identity)

export KO_DOCKER_REPO=547414210802.dkr.ecr.us-east-1.amazonaws.com/styra

.PHONY: build build-local push run update ko-login deploy-ci

build:
	ko build --sbom=none --push=false --base-import-paths --tags $(VERSION) --platform=linux/amd64

build-local:
	ko build --sbom=none --local --base-import-paths --tags $(VERSION) --platform=linux/amd64

push:
	ko build -v --sbom=none --base-import-paths --tags $(VERSION) --platform=linux/amd64

run:
	docker run -it -p 8181:8181 $$(ko build --local) run -s --log-level debug

update:
	go mod edit -replace github.com/open-policy-agent/opa=github.com/StyraInc/opa@load
	go mod tidy

ko-login:
	@echo "Ko Login..."
	@echo $(ECR_USER)
	@echo aws ecr get-login-password --region us-east-1 | ko login 547414210802.dkr.ecr.us-east-1.amazonaws.com --username AWS --password-stdin
	@echo docker pull 547414210802.dkr.ecr.us-east-1.amazonaws.com/styra/load:latest

deploy-ci: ko-login push