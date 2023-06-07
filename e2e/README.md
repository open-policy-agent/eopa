# E2E - End-to-end tests

## Run all tests at top level

```
make eopa
make build-local
export BINARY=<PathToEnterpriseOPAExecutable>
make e2e
```

## Run individual test

```
export BINARY=<PathToEnterpriseOPAExecutable>
cd <test directory>
go test -p 1 -tags e2e . -v -count=1
```
