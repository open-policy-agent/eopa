# E2E - End-to-end tests

## Run all tests at top level

```
make load
make build-local
export BINARY=<PathToLoadExecutable>
make e2e
```

## Run individual test

```
export BINARY=<PathToLoadExecutable>
cd <test directory>
go test -p 1 -tags e2e . -v -count=1
```
