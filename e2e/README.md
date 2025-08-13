# E2E - End-to-end tests

## Run all tests at top level

```
make eopa
export BINARY=<PathToEOPAExecutable>
make e2e
```

## Run individual test

```
export BINARY=<PathToEOPAExecutable>
cd <test directory>
go test -p 1 -tags e2e . -v -count=1
```
