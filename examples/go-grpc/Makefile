ORIGINAL_DIR = $(shell pwd)
PROTOC_PATH ?= $(shell `which protoc`)
PATH := $(PATH):$(PROTOC_PATH)

build: grpcpush

.PHONY: grpcpush
# uncomment for stripped binaries:
grpcpush:
	go build -o "$@" -ldflags="-s -w" ./cmd/grpcpush
# uncomment for regular binaries:
#grpcpush:
#        go build -o "$@" ./cmd/grpcpush

.PHONY: test
test: grpcpush
	go test -timeout 30s ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: lint
lint:
	golint ./...
	errcheck ./...
	ineffassign ./...
	go vet ./...
	govulncheck ./...

.PHONY: coverage
coverage:
	go test -timeout 30s -coverprofile /dev/null ./...

.PHONY: viewcoverage
viewcoverage:
	go test -timeout 30s -coverprofile cover.out ./...
	go tool cover -html=cover.out

.PHONY: benchmark
benchmark:
	go test -bench=.

.PHONY: install
install:
	go install ./...

.PHONY: grpc-gen-go
grpc-gen-go:
	cd ../../proto && \
	buf generate;
	cd ${ORIGINAL_DIR}

.PHONY: clean
clean:
	rm -f ./grpcpush ./cover.out
