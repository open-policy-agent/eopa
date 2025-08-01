<!-- markdownlint-disable MD041 -->
Extract the EOPA tarball into your source directory.

```bash
# terminal-command
mkdir -p eopa
# terminal-command
tar xzvf /path/to/eopa.tar.gz –strip-component=1  -C eopa
```

Then add the following lines to your application's `go.mod` file:

```go-mod
require github.com/styrainc/enterprise-opa-private <VERSION>
```
```go-mod
replace github.com/styrainc/enterprise-opa-private => ./eopa
