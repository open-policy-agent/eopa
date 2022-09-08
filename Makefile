GOPRIVATE=github.com/StyraInc/opa
KO_DOCKER_REPO?=dont-publish-yet
build:
	ko build --push=false

push:
	ko build

run:
	docker run -it -p 8181:8181 $$(ko build --local) run -s

update:
	go mod edit -replace github.com/open-policy-agent/opa=github.com/StyraInc/opa@load
	go mod tidy
