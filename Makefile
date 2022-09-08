KO_DOCKER_REPO?= # FIXME: ghcr.io/StyraInc/load ?
build:
	ko build --push=false

push:
	ko build

run:
	docker run -it -p 8181:8181 $$(ko build --local) run -s
