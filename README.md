# Styra Load

## Build

Prerequisites:

- Golang
- [ko](https://github.com/ko-build/ko), `brew install ko`
- Docker
- Make

Build with `make build`, run with `make run`, publish with `make push`.

## FAQ

### Can't build locally: private github repo

````
go: errors parsing go.mod:
/Users/stephan/Sources/StyraInc/load/go.mod:89: replace github.com/StyraInc/opa: version "load" invalid: git ls-remote -q origin in /Users/stephan/go/pkg/mod/cache/vcs/39c7f8258aa43a0e71284d9afa9390ab62dcf0466b0baf3bc3feef290c1fe63d: exit status 128:
	fatal: could not read Username for 'https://github.com': terminal prompts disabled
Confirm the import path was entered correctly.
If this is a private repository, see https://golang.org/doc/faq#git_https for additional information.
````

Adding this snippet to your .gitconfig should help:
```
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
```
