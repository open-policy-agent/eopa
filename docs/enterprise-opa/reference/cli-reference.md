---
sidebar_position: 4
sidebar_label: CLI Reference
title: CLI Reference
---

# EOPA CLI Reference

The EOPA executable provides the following commands.

## eopa bench

Benchmark a Rego query

### Synopsis

Benchmark a Rego query and print the results.

The benchmark command works very similar to ‘eval’ and will evaluate the
query in the same fashion. The evaluation will be repeated a number of
times and performance results will be returned.

Example with bundle and input data:

```
eopa bench -b ./policy-bundle -i input.json 'data.authz.allow'
```

To run benchmarks against a running EOPA server to evaluate
server overhead use the –e2e flag. To enable more detailed analysis use
the –metrics and –benchmem flags.

The optional “gobench” output format conforms to the Go Benchmark Data
Format.

```
eopa bench <query> [flags]
```

### Options

```
      --benchmem                        report memory allocations with benchmark results (default true)
  -b, --bundle string                   set bundle file(s) or directory path(s). This flag can be repeated.
  -c, --config-file string              set path of configuration file
      --count int                       number of times to repeat each benchmark (default 1)
  -d, --data string                     set policy or data file(s). This flag can be repeated.
      --e2e                             run benchmarks against a running EOPA server
      --fail                            exits with non-zero exit code on undefined/empty result and errors (default true)
  -f, --format {pretty,json,gobench}    set output format (default pretty)
  -h, --help                            help for bench
      --ignore strings                  set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
      --import string                   set query import(s). This flag can be repeated.
  -i, --input string                    set input file path
      --metrics                         report query performance metrics (default true)
      --optimize-store-for-read-speed   optimize default in-memory store for read speed. Has possible negative impact on memory footprint and write speed. See https://www.openpolicyagent.org/docs/latest/policy-performance/#storage-optimization for more details.
      --package string                  set query package
  -p, --partial                         perform partial evaluation
  -s, --schema string                   set schema file path or directory path
      --shutdown-grace-period int       set the time (in seconds) that the server will wait to gracefully shut down. This flag is valid in 'e2e' mode only. (default 10)
      --shutdown-wait-period int        set the time (in seconds) that the server will wait before initiating shutdown. This flag is valid in 'e2e' mode only.
      --stdin                           read query from stdin
  -I, --stdin-input                     read input document from stdin
  -t, --target {rego,wasm}              set the runtime to exercise (default rego)
  -u, --unknowns stringArray            set paths to treat as unknown during partial evaluation (default [input])
      --v0-compatible                   opt-in to OPA features and behaviors prior to the OPA v1.0 release
```

------------------------------------------------------------------------

## eopa build

Build an EOPA bundle

### Synopsis

Build an EOPA bundle.

The ‘build’ command packages EOPA policy and data files into
bundles. Bundles are gzipped tarballs containing policies and data.
Paths referring to directories are loaded recursively.

```
$ ls
example.rego

$ eopa build -b .
```

You can load bundles into EOPA on the command-line:

```
$ ls
bundle.tar.gz example.rego

$ eopa run bundle.tar.gz
```

You can also configure EOPA to download bundles from remote
HTTP endpoints:

```
$ eopa run --server \
    --set bundles.example.resource=bundle.tar.gz \
    --set services.example.url=http://localhost:8080
```

Inside another terminal in the same directory, serve the bundle via
HTTP:

```
$ python3 -m http.server --bind localhost 8080
```

For more information on bundles see
https://www.openpolicyagent.org/docs/latest/management-bundles/.

### Common Flags

When -b is specified the ‘build’ command assumes paths refer to existing
bundle files or directories following the bundle structure. If multiple
bundles are provided, their contents are merged. If there are any merge
conflicts (e.g., due to conflicting bundle roots), the command fails.
When loading an existing bundle file, the .manifest from the input
bundle will be included in the output bundle. Flags that set .manifest
fields (such as –revision) override input bundle .manifest fields.

The -O flag controls the optimization level. By default, optimization is
disabled (-O=0). When optimization is enabled the ‘build’ command
generates a bundle that is semantically equivalent to the input files
however the structure of the files in the bundle may have been changed
by rewriting, inlining, pruning, etc. Higher optimization levels may
result in longer build times. The –partial-namespace flag can used in
conjunction with the -O flag to specify the namespace for the partially
evaluated files in the optimized bundle.

The ‘build’ command supports targets (specified by -t):

```
rego    The default target emits a bundle containing a set of policy and data files
        that are semantically equivalent to the input files. If optimizations are
        disabled the output may simply contain a copy of the input policy and data
        files. If optimization is enabled at least one entrypoint must be supplied,
        either via the -e option, or via entrypoint metadata annotations.

wasm    The wasm target emits a bundle containing a WebAssembly module compiled from
        the input files for each specified entrypoint. The bundle may contain the
        original policy or data files.

plan    The plan target emits a bundle containing a plan, i.e., an intermediate
        representation compiled from the input files for each specified entrypoint.
        This is for further processing, EOPA cannot evaluate a "plan bundle" like it
        can evaluate a wasm or rego bundle.
```

The -e flag tells the ‘build’ command which documents (entrypoints) will
be queried by the software asking for policy decisions, so that it can
focus optimization efforts and ensure that document is not eliminated by
the optimizer. Note: Unless the –prune-unused flag is used, any rule
transitively referring to a package or rule declared as an entrypoint
will also be enumerated as an entrypoint.

### Signing

The ‘build’ command can be used to verify the signature of a signed
bundle and also to generate a signature for the output bundle the
command creates.

If the directory path(s) provided to the ‘build’ command contain a
“.signatures.json” file, it will attempt to verify the signatures
included in that file. The bundle files or directory path(s) to verify
must be specified using –bundle.

For more information on the bundle signing and verification, see
https://www.openpolicyagent.org/docs/latest/management-bundles/#signing.

Example:

```
$ eopa build --verification-key /path/to/public_key.pem --signing-key /path/to/private_key.pem --bundle foo
```

Where foo has the following structure:

```
foo/
  |
  +-- bar/
  |     |
  |     +-- data.json
  |
  +-- policy.rego
  |
  +-- .manifest
  |
  +-- .signatures.json
```

The ‘build’ command will verify the signatures using the public key
provided by the –verification-key flag. The default signing algorithm is
RS256 and the –signing-alg flag can be used to specify a different one.
The –verification-key-id and –scope flags can be used to specify the
name for the key provided using the –verification-key flag and scope to
use for bundle signature verification respectively.

If the verification succeeds, the ‘build’ command will write out an
updated “.signatures.json” file to the output bundle. It will use the
key specified by the –signing-key flag to sign the token in the
“.signatures.json” file.

To include additional claims in the payload use the –claims-file flag to
provide a JSON file containing optional claims.

For more information on the format of the “.signatures.json” file see
https://www.openpolicyagent.org/docs/latest/management-bundles/#signature-format.

### Capabilities

The ‘build’ command can validate policies against a configurable set of
EOPA capabilities. The capabilities define the built-in
functions and other language features that policies may depend on. For
example, the following capabilities file only permits the policy to
depend on the “plus” built-in function (‘+’):

```
{
    "builtins": [
        {
            "name": "plus",
            "infix": "+",
            "decl": {
                "type": "function",
                "args": [
                    {
                        "type": "number"
                    },
                    {
                        "type": "number"
                    }
                ],
                "result": {
                    "type": "number"
                }
            }
        }
    ]
}
```

Capabilities can be used to validate policies against a specific version
of EOPA. The EOPA repository contains a set of
capabilities files for each EOPA release. For example, the
following command builds a directory of policies (‘./policies’) and
validates them against EOPA v0.22.0:

```
eopa build ./policies --capabilities v0.22.0
```


```
eopa build <path> [<path> [...]] [flags]
```

### Options

```
  -b, --bundle                         load paths as bundle files or root directories
      --capabilities string            set capabilities version or capabilities.json file path
      --claims-file string             set path of JSON file containing optional claims (see: https://www.openpolicyagent.org/docs/latest/management-bundles/#signature-format)
      --debug                          enable debug output
  -e, --entrypoint string              set slash separated entrypoint path
      --exclude-files-verify strings   set file names to exclude during bundle verification
      --follow-symlinks                follow symlinks in the input set of paths when building the bundle
  -h, --help                           help for build
      --ignore strings                 set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
  -O, --optimize int                   set optimization level
  -o, --output string                  set the output filename (default "bundle.tar.gz")
      --partial-namespace string       set the namespace to use for partially evaluated files in an optimized bundle (default "partial")
      --prune-unused                   exclude dependents of entrypoints
  -r, --revision string                set output bundle revision
      --scope string                   scope to use for bundle signature verification
      --signing-alg string             name of the signing algorithm (default "RS256")
      --signing-key string             set the secret (HMAC) or path of the PEM file containing the private key (RSA and ECDSA)
      --signing-plugin string          name of the plugin to use for signing/verification (see https://www.openpolicyagent.org/docs/latest/management-bundles/#signature-plugin)
  -t, --target {rego,wasm,plan}        set the output bundle target type (default rego)
      --v0-compatible                  opt-in to OPA features and behaviors prior to the OPA v1.0 release
      --verification-key string        set the secret (HMAC) or path of the PEM file containing the public key (RSA and ECDSA)
      --verification-key-id string     name assigned to the verification key used for bundle verification (default "default")
      --wasm-include-print             enable print statements inside of WebAssembly modules compiled by the compiler
```

------------------------------------------------------------------------

## eopa bundle

EOPA Bundle commands

### Options

```
  -h, --help   help for bundle
```

------------------------------------------------------------------------

## eopa bundle convert

Convert OPA bundle to binary bundle

```
eopa bundle convert <path to input bundle> <path to output converted bundle> [flags]
```

### Options

```
  -h, --help   help for convert
```

------------------------------------------------------------------------

## eopa bundle dump

Dump binary bundle data

```
eopa bundle dump [flags]
```

### Options

```
  -h, --help   help for dump
```

------------------------------------------------------------------------

## eopa capabilities

Print the capabilities of EOPA

### Synopsis

Show capabilities for EOPA.

The ‘capabilities’ command prints the EOPA capabilities, prior
to and including the version of EOPA used.

Print a list of all existing capabilities version names

```
$ eopa capabilities
v0.17.0
v0.17.1
...
v0.37.1
v0.37.2
v0.38.0
...
```

Print the capabilities of the current version

```
$ eopa capabilities --current
{
    "builtins": [...],
    "future_keywords": [...],
    "wasm_abi_versions": [...]
}
```

Print the capabilities of a specific version

```
$ eopa capabilities --version v0.32.1
{
    "builtins": [...],
    "future_keywords": null,
    "wasm_abi_versions": [...]
}
```

Print the capabilities of a capabilities file

```
$ eopa capabilities --file ./capabilities/v0.32.1.json
{
    "builtins": [...],
    "future_keywords": null,
    "wasm_abi_versions": [...]
}
```


```
eopa capabilities [flags]
```

### Options

```
      --current          print current capabilities
      --file string      print capabilities defined by a file
  -h, --help             help for capabilities
      --v0-compatible    opt-in to OPA features and behaviors prior to the OPA v1.0 release
      --version string   print capabilities of a specific version
```

------------------------------------------------------------------------

## eopa check

Check Rego source files

### Synopsis

Check Rego source files for parse and compilation errors.

If the ‘check’ command succeeds in parsing and compiling the source
file(s), no output is produced. If the parsing or compiling fails,
‘check’ will output the errors and exit with a non-zero exit code.

```
eopa check <path> [path [...]] [flags]
```

### Options

```
  -b, --bundle                 load paths as bundle files or root directories
      --capabilities string    set capabilities version or capabilities.json file path
  -f, --format {pretty,json}   set output format (default pretty)
  -h, --help                   help for check
      --ignore strings         set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
  -m, --max-errors int         set the number of errors to allow before compilation fails early (default 10)
  -s, --schema string          set schema file path or directory path
  -S, --strict                 enable compiler strict mode
      --v0-compatible          opt-in to OPA features and behaviors prior to the OPA v1.0 release
      --v0-v1                  check for Rego v0 and v1 compatibility (policies must be compatible with both Rego versions)
```

------------------------------------------------------------------------

## eopa deps

Analyze Rego query dependencies

### Synopsis

Print dependencies of provided query.

Dependencies are categorized as either base documents, which is any data
loaded from the outside world, or virtual documents, i.e values that are
computed from rules.

```
eopa deps <query> [flags]
```

### Examples

```

Given a policy like this:

    package policy

    allow if is_admin

    is_admin if "admin" in input.user.roles

To evaluate the dependencies of a simple query (e.g. data.policy.allow),
we'd run eopa deps like demonstrated below:

    $ eopa deps --data policy.rego data.policy.allow
    +------------------+----------------------+
    |  BASE DOCUMENTS  |  VIRTUAL DOCUMENTS   |
    +------------------+----------------------+
    | input.user.roles | data.policy.allow    |
    |                  | data.policy.is_admin |
    +------------------+----------------------+

From the output we're able to determine that the allow rule depends on
the input.user.roles base document, as well as the virtual document (rule)
data.policy.is_admin.

```

### Options

```
  -b, --bundle string          set bundle file(s) or directory path(s). This flag can be repeated.
  -d, --data string            set policy or data file(s). This flag can be repeated.
  -f, --format {pretty,json}   set output format (default pretty)
  -h, --help                   help for deps
      --ignore strings         set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
```

------------------------------------------------------------------------

## eopa eval

Evaluate a Rego query

### Synopsis

Evaluate a Rego query and print the result. \### Optimization Flags

The -O flag controls the optimization level. By default, only a limited
selection of the safest optimizations are enabled at -O=0, with
progressively more aggressive optimizations enabled at successively
higher -O levels.

Nearly all optimizations can be controlled directly with enable/disable
flags. The pattern for these flags mimics that of well-known compilers,
with -of and -ofno prefixes controlling enabling and disabling of
specific passes, respectively.

The following flags control specific optimizations:

-oflicm/-ofno-licm Controls the Loop-Invariant Code Motion (LICM) pass.
LICM is used to automatically pull loop-independent code out of loops,
dramatically improving performance for most iteration-heavy policies.
(Enabled by default at -O=0)

```
eopa eval <query> [flags]
```

### Examples

```


To evaluate a simple query:

    $ eopa eval 'x := 1; y := 2; x < y'

To evaluate a query against JSON data:

    $ eopa eval --data data.json 'name := data.names[_]'

To evaluate a query against JSON data supplied with a file:// URL:

    $ eopa eval --data file:///path/to/file.json 'data'


### File & Bundle Loading


The --bundle flag will load data files and Rego files contained
in the bundle specified by the path. It can be either a
compressed tar archive bundle file or a directory tree.

    $ eopa eval --bundle /some/path 'data'

Where /some/path contains:

    foo/
      |
      +-- bar/
      |     |
      |     +-- data.json
      |
      +-- baz.rego
      |
      +-- manifest.yaml

The JSON file 'foo/bar/data.json' would be loaded and rooted under
'data.foo.bar' and the 'foo/baz.rego' would be loaded and rooted under the
package path contained inside the file. Only data files named data.json or
data.yaml will be loaded. In the example above the manifest.yaml would be
ignored.

See https://www.openpolicyagent.org/docs/latest/management-bundles/ for more details
on bundle directory structures.

The --data flag can be used to recursively load ALL *.rego, *.json, and
*.yaml files under the specified directory.

The -O flag controls the optimization level. By default, optimization is disabled (-O=0).
When optimization is enabled the 'eval' command generates a bundle from the files provided
with either the --bundle or --data flag. This bundle is semantically equivalent to the input
files however the structure of the files in the bundle may have been changed by rewriting, inlining,
pruning, etc. This resulting optimized bundle is used to evaluate the query. If optimization is
enabled at least one entrypoint must be supplied, either via the -e option, or via entrypoint
metadata annotations.

### Output Formats


Set the output format with the --format flag.

    --format=json      : output raw query results as JSON
    --format=values    : output line separated JSON arrays containing expression values
    --format=bindings  : output line separated JSON objects containing variable bindings
    --format=pretty    : output query results in a human-readable format
    --format=source    : output partial evaluation results in a source format
    --format=raw       : output the values from query results in a scripting friendly format
    --format=discard   : output the result field as "discarded" when non-nil

### Schema


The -s/--schema flag provides one or more JSON Schemas used to validate references to the input or data documents.
Loads a single JSON file, applying it to the input document; or all the schema files under the specified directory.

    $ eopa eval --data policy.rego --input input.json --schema schema.json
    $ eopa eval --data policy.rego --input input.json --schema schemas/

### Capabilities


When passing a capabilities definition file via --capabilities, one can restrict which
hosts remote schema definitions can be retrieved from. For example, a capabilities.json
containing

    {
        "builtins": [ ... ],
        "allow_net": [ "kubernetesjsonschema.dev" ]
    }

would disallow fetching remote schemas from any host but "kubernetesjsonschema.dev".
Setting allow_net to an empty array would prohibit fetching any remote schemas.

Not providing a capabilities file, or providing a file without an allow_net key, will
permit fetching remote schemas from any host.

Note that the metaschemas http://json-schema.org/draft-04/schema, http://json-schema.org/draft-06/schema,
and http://json-schema.org/draft-07/schema, are always available, even without network
access.

```

### Options

```
  -b, --bundle string                                             set bundle file(s) or directory path(s). This flag can be repeated.
      --capabilities string                                       set capabilities version or capabilities.json file path
      --count int                                                 number of times to repeat each benchmark (default 1)
      --coverage                                                  report coverage
  -d, --data string                                               set policy or data file(s). This flag can be repeated.
      --disable-early-exit                                        disable 'early exit' optimizations
      --disable-indexing                                          disable indexing optimizations
      --disable-inlining stringArray                              set paths of documents to exclude from inlining
  -e, --entrypoint string                                         set slash separated entrypoint path
      --explain {off,full,notes,fails,debug}                      enable query explanations (default off)
      --fail                                                      exits with non-zero exit code on undefined/empty result and errors
      --fail-defined                                              exits with non-zero exit code on defined/non-empty result and errors
  -f, --format {json,values,bindings,pretty,source,raw,discard}   set output format (default json)
  -h, --help                                                      help for eval
      --ignore strings                                            set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
      --import string                                             set query import(s). This flag can be repeated.
  -i, --input string                                              set input file path
      --instruction-limit int                                     set instruction limit for VM (default 100000000)
      --instrument                                                enable query instrumentation metrics (implies --metrics)
      --license-key string                                        Location of file containing EOPA_LICENSE_KEY
      --license-token string                                      Location of file containing EOPA_LICENSE_TOKEN
      --log-format {json,json-pretty}                             set log format (default json)
  -l, --log-level {debug,info,error}                              set log level (default info)
      --metrics                                                   report query performance metrics
      --nondeterminstic-builtins                                  evaluate nondeterministic builtins (if all arguments are known) during partial eval
  -O, --optimize int                                              set optimization level
      --optimize-store-for-read-speed                             optimize default in-memory store for read speed. Has possible negative impact on memory footprint and write speed. See https://www.openpolicyagent.org/docs/latest/policy-performance/#storage-optimization for more details.
      --package string                                            set query package
  -p, --partial                                                   perform partial evaluation
      --pretty-limit int                                          set limit after which pretty output gets truncated (default 80)
      --profile                                                   perform expression profiling
      --profile-limit int                                         set number of profiling results to show (default 10)
      --profile-sort string                                       set sort order of expression profiler results. Accepts: total_time_ns, num_eval, num_redo, num_gen_expr, file, line. This flag can be repeated.
  -s, --schema string                                             set schema file path or directory path
      --shallow-inlining                                          disable inlining of rules that depend on unknowns
      --show-builtin-errors                                       collect and return all encountered built-in errors, built in errors are not fatal
      --stdin                                                     read query from stdin
  -I, --stdin-input                                               read input document from stdin
  -S, --strict                                                    enable compiler strict mode
      --strict-builtin-errors                                     treat the first built-in function error encountered as fatal
  -t, --target {rego,wasm}                                        set the runtime to exercise (default rego)
      --timeout duration                                          set eval timeout (default unlimited)
  -u, --unknowns stringArray                                      set paths to treat as unknown during partial evaluation (default [input])
      --v0-compatible                                             opt-in to OPA features and behaviors prior to the OPA v1.0 release
      --var-values                                                show local variable values in pretty trace output
```

------------------------------------------------------------------------

## eopa exec

Execute against input files

### Synopsis

Execute against input files.

The ‘exec’ command executes EOPA against one or more input
files. If the paths refer to directories, EOPA will execute
against files contained inside those directories, recursively.

The ‘exec’ command accepts a –config-file/-c or series of –set options
as arguments. These options behave the same as way as ‘eopa run’. Since
the ‘exec’ command is intended to execute EOPA in one-shot,
the ‘exec’ command will manually trigger plugins before and after policy
execution:

Before: Discovery -\> Bundle -\> Status After: Decision Logs

By default, the ‘exec’ command executes the “default decision”
(specified in the EOPA configuration) against each input file.
This can be overridden by specifying the –decision argument and pointing
at a specific policy decision,

e.g., eopa exec –decision /foo/bar/baz …

### Optimization Flags

The -O flag controls the optimization level. By default, only a limited
selection of the safest optimizations are enabled at -O=0, with
progressively more aggressive optimizations enabled at successively
higher -O levels.

Nearly all optimizations can be controlled directly with enable/disable
flags. The pattern for these flags mimics that of well-known compilers,
with -of and -ofno prefixes controlling enabling and disabling of
specific passes, respectively.

The following flags control specific optimizations:

-oflicm/-ofno-licm Controls the Loop-Invariant Code Motion (LICM) pass.
LICM is used to automatically pull loop-independent code out of loops,
dramatically improving performance for most iteration-heavy policies.
(Enabled by default at -O=0)

```
eopa exec <path> [<path> [...]] [flags]
```

### Examples

```
  Loading input from stdin:
    eopa exec [<path> [...]] --stdin-input [flags]

```

### Options

```
  -b, --bundle string                        set bundle file(s) or directory path(s). This flag can be repeated.
  -c, --config-file string                   set path of configuration file
      --decision string                      set decision to evaluate
      --fail                                 exits with non-zero exit code on undefined/empty result and errors
      --fail-defined                         exits with non-zero exit code on defined/non-empty result and errors
  -f, --format {json}                        set output format (default json)
  -h, --help                                 help for exec
      --instruction-limit int                set instruction limit for VM (default 100000000)
      --license-key string                   Location of file containing EOPA_LICENSE_KEY
      --license-token string                 Location of file containing EOPA_LICENSE_TOKEN
      --log-format {text,json,json-pretty}   set log format (default json)
  -l, --log-level {debug,info,error}         set log level (default error)
      --log-timestamp-format string          set log timestamp format (OPA_LOG_TIMESTAMP_FORMAT environment variable)
      --no-license-fallback                  Don't fall back to OPA-mode when no license provided.
  -O, --optimize int                         set optimization level
      --set stringArray                      override config values on the command line (use commas to specify multiple values)
      --set-file stringArray                 override config values with files on the command line (use commas to specify multiple values)
      --timeout duration                     set exec timeout with a Go-style duration, such as '5m 30s'. (default unlimited)
      --v0-compatible                        opt-in to OPA features and behaviors prior to the OPA v1.0 release
```

------------------------------------------------------------------------

## eopa fmt

Format Rego source files

### Synopsis

Format Rego source files.

The ‘fmt’ command takes a Rego source file and outputs a reformatted
version. If no file path is provided - this tool will use stdin. The
format of the output is not defined specifically; whatever this tool
outputs is considered correct format (with the exception of bugs).

If the ‘-w’ option is supplied, the ‘fmt’ command will overwrite the
source file instead of printing to stdout.

If the ‘-d’ option is supplied, the ‘fmt’ command will output a diff
between the original and formatted source.

If the ‘-l’ option is supplied, the ‘fmt’ command will output the names
of files that would change if formatted. The ‘-l’ option will suppress
any other output to stdout from the ‘fmt’ command.

If the ‘–fail’ option is supplied, the ‘fmt’ command will return a non
zero exit code if a file would be reformatted.

The ‘fmt’ command can be run in several compatibility modes for
consuming and outputting different Rego versions:

-   `opa fmt`:
    -   v1 Rego is formatted to v1
    -   `rego.v1`/`future.keywords` imports are NOT removed
    -   `rego.v1`/`future.keywords` imports are NOT added if missing
    -   v0 rego is rejected
-   `opa fmt --v0-compatible`:
    -   v0 Rego is formatted to v0
    -   v1 Rego is rejected
-   `opa fmt --v0-v1`:
    -   v0 Rego is formatted to be compatible with v0 AND v1
    -   v1 Rego is rejected
-   `opa fmt --v0-v1 --v1-compatible`:
    -   v1 Rego is formatted to be compatible with v0 AND v1
    -   v0 Rego is rejected

```
eopa fmt [path [...]] [flags]
```

### Options

```
      --capabilities string   set capabilities version or capabilities.json file path
      --check-result          assert that the formatted code is valid and can be successfully parsed (default true)
  -d, --diff                  only display a diff of the changes
      --drop-v0-imports       drop v0 imports from the formatted code, such as 'rego.v1' and 'future.keywords'
      --fail                  non zero exit code on reformat
  -h, --help                  help for fmt
  -l, --list                  list all files who would change when formatted
      --v0-compatible         opt-in to OPA features and behaviors prior to the OPA v1.0 release
      --v0-v1                 format module(s) to be compatible with both Rego v0 and v1
  -w, --write                 overwrite the original source file
```

------------------------------------------------------------------------

## eopa impact

Live Impact Analysis control

### Options

```
  -h, --help   help for impact
```

------------------------------------------------------------------------

## eopa impact record

Start recording

```
eopa impact record [flags]
```

### Options

```
  -a, --addr string                   EOPA address to connect to (e.g. "https://staging.enterprise-opa.example.com:8443") (default "http://127.0.0.1:8181")
  -b, --bundle string                 Path to bundle to use for secondary evaluation
  -d, --duration duration             Live Impact Analysis duration (e.g. "5m") (default 30s)
      --equals                        Include equal results (e.g. for assessing performance differences)
      --fail-any                      Fail if there's any finding (exit 1)
  -f, --format string                 output format: "json", "csv", or "pretty") (default "pretty")
      --group                         Group report by path and input
  -h, --help                          help for record
      --limit int                     Limit report to N rows (if grouped, ordered by count descending)
  -o, --output string                 write report to file, "-" means stdout (default "-")
      --sample-rate float             Sample rate of evaluations to include (e.g. 0.1 for 10%, or 1 for all requests) (default 0.1)
      --tls-ca-cert-file string       TLS CA cert path
      --tls-cert-file string          TLS client cert path
      --tls-private-key-file string   TLS key path
      --tls-skip-verification         Skip TLS verification when connecting to EOPA
```

------------------------------------------------------------------------

## eopa inspect

Inspect EOPA bundle(s)

### Synopsis

Inspect EOPA bundle(s).

The ‘inspect’ command provides a summary of the contents in Enterprise
OPA bundle(s) or a single Rego file. Bundles are gzipped tarballs
containing policies and data. The ‘inspect’ command reads bundle(s) and
lists the following:

-   packages that are contributed by .rego files
-   data locations defined by the data.json and data.yaml files
-   manifest data
-   signature data
-   information about the Wasm module files
-   package- and rule annotations

Example:

```
$ ls
bundle.tar.gz
$ eopa inspect bundle.tar.gz
```

You can provide exactly one EOPA bundle, to a bundle
directory, or direct path to a Rego file to the ‘inspect’ command on the
command-line. If you provide a path referring to a directory, the
‘inspect’ command will load that path as a bundle and summarize its
structure and contents. If you provide a path referring to a Rego file,
the ‘inspect’ command will load that file and summarize its structure
and contents.

```
eopa inspect <path> [<path> [...]] [flags]
```

### Options

```
  -a, --annotations            list annotations
  -f, --format {pretty,json}   set output format (default pretty)
  -h, --help                   help for inspect
      --v0-compatible          opt-in to OPA features and behaviors prior to the OPA v1.0 release
```

------------------------------------------------------------------------

## eopa license

License status

### Synopsis

View details about an EOPA license key or token.

```
eopa license [flags]
```

### Options

```
  -h, --help                   help for license
      --license-key string     Location of file containing EOPA_LICENSE_KEY
      --license-token string   Location of file containing EOPA_LICENSE_TOKEN
```

------------------------------------------------------------------------

## eopa license trial

Create a new EOPA trial license.

### Synopsis

Gather all of the data needed to create a new EOPA trial
license and create one. Any information not provided via flags is
collected interactively. Upon success, the new trial license key is
printed to stdout.

```
eopa license trial [flags]
```

### Options

```
      --company string      the company name to attach to the trial license
      --country string      the country to attach to the trial license
      --email string        a work email address to attach to the trial license
      --first-name string   first name to attach to the trial license
  -h, --help                help for trial
      --key-only            on success, print only the license key to stdout
      --last-name string    last name to attach to the trial license
```

------------------------------------------------------------------------

## eopa lint

Lint Rego source files

### Synopsis

Lint Rego source files for linter rule violations.

```
eopa lint <path> [path [...]] [flags]
```

### Options

```
  -c, --config-file string        set path of configuration file
      --debug                     enable debug logging (including print output from custom policy)
  -d, --disable string            disable specific rule(s). This flag can be repeated.
  -D, --disable-all               disable all rules
      --disable-category string   disable all rules in a category. This flag can be repeated.
  -e, --enable string             enable specific rule(s). This flag can be repeated.
  -E, --enable-all                enable all rules
      --enable-category string    enable all rules in a category. This flag can be repeated.
      --enable-print              enable print output from policy
  -l, --fail-level string         set level at which to fail with a non-zero exit code (error, warning) (default "error")
  -f, --format string             set output format (pretty, compact, json, github, sarif) (default "pretty")
  -h, --help                      help for lint
      --ignore-files string       ignore all files matching a glob-pattern. This flag can be repeated.
      --instrument                enable instrumentation metrics to be added to reporting (currently supported only for JSON output format)
      --metrics                   enable metrics reporting (currently supported only for JSON output format)
      --no-color                  Disable color output
  -o, --output-file string        set file to use for linting output, defaults to stdout
      --pprof string              enable profiling (must be one of cpu, clock, mem_heap, mem_allocs, trace, goroutine, mutex, block, thread_creation)
      --profile                   enable profiling metrics to be added to reporting (currently supported only for JSON output format)
  -r, --rules string              set custom rules file(s). This flag can be repeated.
      --timeout duration          set timeout for linting (default unlimited)
```

------------------------------------------------------------------------

## eopa login

Sign-in to DAS instance

```
eopa login [flags]
```

### Examples

```

Create a new browser session that is shared with EOPA.

Using settings from .styra.yaml:

    eopa login

Note: 'eopa login' will look for .styra.yaml in the current directory,
the repository root, and your home directory. To use a different config
file location, pass --styra-config:

    eopa login --styra-config ~/.strya-primary.yaml

You can also provide your DAS endpoint via a flag:

    eopa login --url https://my-tenant.styra.com

On successful login, a .styra.yaml file will be generated in your current
working directory.

If the automatic token transfer fails, the browser tab will show you the
token to use. Paste the token into the following command to have it stored
manually:

    eopa login --read-token

```

### Options

```
  -h, --help                  help for login
      --libraries string      where to copy libraries to (default ".styra/include")
      --log-format string     log format (default "text")
      --log-level string      log level (default "info")
      --no-open               do not attempt to open a browser window
      --read-token            read token from stdin
      --secret-file string    file to store the secret in
      --styra-config string   Styra DAS config file to use
      --timeout duration      timeout waiting for a browser callback event (default 1m0s)
      --url string            DAS address to connect to (e.g. "https://my-tenant.styra.com")
```

------------------------------------------------------------------------

## eopa parse

Parse Rego source file

### Synopsis

Parse Rego source file and print AST.

```
eopa parse <path> [flags]
```

### Options

```
  -f, --format {pretty,json}   set output format (default pretty)
  -h, --help                   help for parse
      --json-include string    include or exclude optional elements. By default comments are included. Current options: locations, comments. E.g. --json-include locations,-comments will include locations and exclude comments.
      --v0-compatible          opt-in to OPA features and behaviors prior to the OPA v1.0 release
```

------------------------------------------------------------------------

## eopa pull

Pull libraries from DAS instance

```
eopa pull [flags]
```

### Examples

```

Download all DAS libraries using settings from .styra.yaml:

    eopa pull

Note: 'eopa pull' will look for .styra.yaml in the current directory,
the repository root, and your home directory. To use a different config
file location, pass --styra-config:

    eopa pull --styra-config ~/.styra-primary.yaml

If the environment varable EOPA_STYRA_DAS_TOKEN is set, 'eopa pull'
will use it as an API token to talk to the configured DAS instance:

    EOPA_STYRA_DAS_TOKEN="..." eopa pull

Write all libraries to to libs/, with debug logging enabled:

    eopa pull --libraries libs --log-level debug

Remove files that aren't expected in the target directory:

    eopa pull --force

```

### Options

```
  -f, --force                 ignore if libraries folder exists, overwrite existing content on conflict
  -h, --help                  help for pull
      --libraries string      where to copy libraries to (default ".styra/include")
      --log-format string     log format (default "text")
      --log-level string      log level (default "info")
      --secret-file string    file to store the secret in
      --styra-config string   Styra DAS config file to use
      --url string            DAS address to connect to (e.g. "https://my-tenant.styra.com")
```

------------------------------------------------------------------------

## eopa run

Start EOPA in interactive or server mode

### Synopsis

Start an instance of EOPA.

To run the interactive shell:

```
$ eopa run
```

To run the server:

```
$ eopa run -s
```

The ‘run’ command starts an instance of the EOPA runtime. The
EOPA runtime can be started as an interactive shell or a
server.

When the runtime is started as a shell, users can define rules and
evaluate expressions interactively. When the runtime is started as a
server, EOPA exposes an HTTP API for managing policies,
reading and writing data, and executing queries.

The runtime can be initialized with one or more files that contain
policies or data. If the ‘–bundle’ option is specified the paths will be
treated as policy bundles and loaded following standard bundle
conventions. The path can be a compressed archive file or a directory
which will be treated as a bundle. Without the ‘–bundle’ flag Enterprise
OPA will recursively load ALL rego, JSON, and YAML files.

When loading from directories, only files with known extensions are
considered. The current set of file extensions that EOPA will
consider are:

```
.json          # JSON data
.yaml or .yml  # YAML data
.rego          # Rego file
```

Non-bundle data file and directory paths can be prefixed with the
desired destination in the data document with the following syntax:

```
<dotted-path>:<file-path>
```

To set a data file as the input document in the interactive shell use
the “repl.input” path prefix with the input file:

```
repl.input:<file-path>
```

Example:

```
$ eopa run repl.input:input.json
```

Which will load the “input.json” file at path “data.repl.input”.

Use the “help input” command in the interactive shell to see more
options.

File paths can be specified as URLs to resolve ambiguity in paths
containing colons:

```
$ eopa run file:///c:/path/to/data.json
```

URL paths to remote public bundles (http or https) will be parsed as
shorthand configuration equivalent of using repeated –set flags to
accomplish the same:

```
$ eopa run -s https://example.com/bundles/bundle.tar.gz
```

The above shorthand command is identical to:

```
$ eopa run -s --set "services.cli1.url=https://example.com" \
             --set "bundles.cli1.service=cli1" \
             --set "bundles.cli1.resource=/bundles/bundle.tar.gz" \
             --set "bundles.cli1.persist=true"
```

The ‘run’ command can also verify the signature of a signed bundle. A
signed bundle is a normal EOPA bundle that includes a file
named “.signatures.json”. For more information on signed bundles see
https://www.openpolicyagent.org/docs/latest/management-bundles/#signing.

The key to verify the signature of signed bundle can be provided using
the –verification-key flag. For example, for RSA family of algorithms,
the command expects a PEM file containing the public key. For HMAC
family of algorithms (eg. HS256), the secret can be provided using the
–verification-key flag.

The –verification-key-id flag can be used to optionally specify a name
for the key provided using the –verification-key flag.

The –signing-alg flag can be used to specify the signing algorithm. The
‘run’ command uses RS256 (by default) as the signing algorithm.

The –scope flag can be used to specify the scope to use for bundle
signature verification.

Example:

```
$ eopa run --verification-key secret --signing-alg HS256 --bundle bundle.tar.gz
```

The ‘run’ command will read the bundle “bundle.tar.gz”, check the
“.signatures.json” file and perform verification using the provided key.
An error will be generated if “bundle.tar.gz” does not contain a
“.signatures.json” file. For more information on the bundle verification
process see
https://www.openpolicyagent.org/docs/latest/management-bundles/#signature-verification.

The ‘run’ command can ONLY be used with the –bundle flag to verify
signatures for existing bundle files or directories following the bundle
structure.

To skip bundle verification, use the –skip-verify flag.

The –watch flag can be used to monitor policy and data file-system
changes. When a change is detected, the updated policy and data is
reloaded into OPA. Watching individual files (rather than directories)
is generally not recommended as some updates might cause them to be
dropped by OPA.

OPA will automatically perform type checking based on a schema inferred
from known input documents and report any errors resulting from the
schema check. Currently this check is performed on OPA’s Authorization
Policy Input document and will be expanded in the future. To disable
this, use the –skip-known-schema-check flag.

The –v0-compatible flag can be used to opt-in to OPA features and
behaviors that were the default in OPA v0.x. Behaviors enabled by this
flag include: - setting OPA’s listening address to “:8181” by default,
corresponding to listening on every network interface. - expecting v0
Rego syntax in policy modules instead of the default v1 Rego syntax.

The –tls-cipher-suites flag can be used to specify the list of enabled
TLS 1.0–1.2 cipher suites. Note that TLS 1.3 cipher suites are not
configurable. Following are the supported TLS 1.0 - 1.2 cipher suites
(IANA): TLS_RSA_WITH_RC4_128_SHA, TLS_RSA_WITH_3DES_EDE_CBC_SHA,
TLS_RSA_WITH_AES_128_CBC_SHA, TLS_RSA_WITH_AES_256_CBC_SHA,
TLS_RSA_WITH_AES_128_CBC_SHA256, TLS_RSA_WITH_AES_128_GCM_SHA256,
TLS_RSA_WITH_AES_256_GCM_SHA384, TLS_ECDHE_ECDSA_WITH_RC4_128_SHA,
TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA, TLS_ECDHE_RSA_WITH_RC4_128_SHA,
TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA, TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256

See https://godoc.org/crypto/tls#pkg-constants for more information.

### Optimization Flags

The -O flag controls the optimization level. By default, only a limited
selection of the safest optimizations are enabled at -O=0, with
progressively more aggressive optimizations enabled at successively
higher -O levels.

Nearly all optimizations can be controlled directly with enable/disable
flags. The pattern for these flags mimics that of well-known compilers,
with -of and -ofno prefixes controlling enabling and disabling of
specific passes, respectively.

The following flags control specific optimizations:

-oflicm/-ofno-licm Controls the Loop-Invariant Code Motion (LICM) pass.
LICM is used to automatically pull loop-independent code out of loops,
dramatically improving performance for most iteration-heavy policies.
(Enabled by default at -O=0)

```
eopa run [flags]
```

### Options

```
  -a, --addr strings                         set listening address of the server (e.g., [ip]:<port> for TCP, unix://<path> for UNIX domain socket) (default [localhost:8181])
      --authentication {token,tls,off}       set authentication scheme (default off)
      --authorization {basic,off}            set authorization scheme (default off)
  -b, --bundle                               load paths as bundle files or root directories
  -c, --config-file string                   set path of configuration file
      --diagnostic-addr strings              set read-only diagnostic listening address of the server for /health and /metric APIs (e.g., [ip]:<port> for TCP, unix://<path> for UNIX domain socket)
      --disable-telemetry                    disables anonymous information reporting (see: https://www.openpolicyagent.org/docs/latest/privacy)
      --exclude-files-verify strings         set file names to exclude during bundle verification
  -f, --format string                        set shell output format, i.e, pretty, json (default "pretty")
      --h2c                                  enable H2C for HTTP listeners
  -h, --help                                 help for run
  -H, --history string                       set path of history file (default "$HOME/.enterprise opa_history")
      --ignore strings                       set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
      --instruction-limit int                set instruction limit for VM (default 100000000)
      --license-discovery-timeout int        Timeout (in seconds) for discovery-based licensing check. (default 30)
      --license-key string                   Location of file containing EOPA_LICENSE_KEY
      --license-token string                 Location of file containing EOPA_LICENSE_TOKEN
      --log-format {text,json,json-pretty}   set log format (default json)
  -l, --log-level {debug,info,error}         set log level (default info)
      --log-timestamp-format string          set log timestamp format (OPA_LOG_TIMESTAMP_FORMAT environment variable)
  -m, --max-errors int                       set the number of errors to allow before compilation fails early (default 10)
      --min-tls-version {1.0,1.1,1.2,1.3}    set minimum TLS version to be used by EOPA's server (default 1.2)
      --no-discovery-license-check           Disable discovery-based licensing check.
      --no-license-fallback                  Don't fall back to OPA-mode when no license provided.
  -O, --optimize int                         set optimization level
      --optimize-store-for-read-speed        optimize default in-memory store for read speed. Has possible negative impact on memory footprint and write speed. See https://www.openpolicyagent.org/docs/latest/policy-performance/#storage-optimization for more details.
      --pprof                                enables pprof endpoints
      --ready-timeout int                    wait (in seconds) for configured plugins before starting server (value <= 0 disables ready check)
      --scope string                         scope to use for bundle signature verification
  -s, --server                               start the runtime in server mode
      --set stringArray                      override config values on the command line (use commas to specify multiple values)
      --set-file stringArray                 override config values with files on the command line (use commas to specify multiple values)
      --shutdown-grace-period int            set the time (in seconds) that the server will wait to gracefully shut down (default 10)
      --shutdown-wait-period int             set the time (in seconds) that the server will wait before initiating shutdown
      --signing-alg string                   name of the signing algorithm (default "RS256")
      --skip-known-schema-check              disables type checking on known input schemas
      --skip-verify                          disables bundle signature verification
      --tls-ca-cert-file string              set path of TLS CA cert file
      --tls-cert-file string                 set path of TLS certificate file
      --tls-cert-refresh-period duration     set certificate refresh period
      --tls-cipher-suites strings            set list of enabled TLS 1.0–1.2 cipher suites (IANA)
      --tls-private-key-file string          set path of TLS private key file
      --unix-socket-perm string              specify the permissions for the Unix domain socket if used to listen for incoming connections (default "755")
      --v0-compatible                        opt-in to OPA features and behaviors prior to the OPA v1.0 release
      --verification-key string              set the secret (HMAC) or path of the PEM file containing the public key (RSA and ECDSA)
      --verification-key-id string           name assigned to the verification key used for bundle verification (default "default")
  -w, --watch                                watch command line files for changes
```

------------------------------------------------------------------------

## eopa sign

Generate an EOPA bundle signature

### Synopsis

Generate an EOPA bundle signature.

The ‘sign’ command generates a digital signature for policy bundles. It
generates a “.signatures.json” file that dictates which files should be
included in the bundle, what their SHA hashes are, and is
cryptographically secure.

The signatures file is a JSON file with an array containing a single
JSON Web Token (JWT) that encapsulates the signature for the bundle.

The –signing-alg flag can be used to specify the algorithm to sign the
token. The ‘sign’ command uses RS256 (by default) as the signing
algorithm. See
https://www.openpolicyagent.org/docs/latest/configuration/#keys for a
list of supported signing algorithms.

The key to be used for signing the JWT MUST be provided using the
–signing-key flag. For example, for RSA family of algorithms, the
command expects a PEM file containing the private key. For HMAC family
of algorithms (eg. HS256), the secret can be provided using the
–signing-key flag.

EOPA ‘sign’ can ONLY be used with the –bundle flag to load
paths that refer to existing bundle files or directories following the
bundle structure.

```
$ eopa sign --signing-key /path/to/private_key.pem --bundle foo
```

Where foo has the following structure:

```
foo/
  |
  +-- bar/
  |     |
  |     +-- data.json
  |
  +-- policy.rego
  |
  +-- .manifest
```

This will create a “.signatures.json” file in the current directory. The
–output-file-path flag can be used to specify a different location for
the “.signatures.json” file.

The content of the “.signatures.json” file is shown below:

```
{
  "signatures": [
    "eyJhbGciOiJSUzI1NiJ9.eyJmaWxlcyI6W3sibmFtZSI6Ii5tYW5pZmVzdCIsImhhc2giOiIxODc0NWRlNzJjMDFlODBjZDlmNTIwZjQxOGMwMDlhYzRkMmMzZDAyYjE3YTUwZTJkMDQyMTU4YmMzNTJhMzJkIiwiYWxnb3JpdGhtIjoiU0hBLTI1NiJ9LHsibmFtZSI6ImJhci9kYXRhLmpzb24iLCJoYXNoIjoiOTNhMjM5NzFhOTE0ZTVlYWNiZjBhOGQyNTE1NGNkYTMwOWMzYzFjNzJmYmI5OTE0ZDQ3YzYwZjNjYjY4MTU4OCIsImFsZ29yaXRobSI6IlNIQS0yNTYifSx7Im5hbWUiOiJwb2xpY3kucmVnbyIsImhhc2giOiJkMGYyNDJhYWUzNGRiNTRlZjU2NmJlYTRkNDVmY2YxOTcwMGM1ZDhmODdhOWRiOTMyZGZhZDZkMWYwZjI5MWFjIiwiYWxnb3JpdGhtIjoiU0hBLTI1NiJ9XX0.lNsmRqrmT1JI4Z_zpY6IzHRZQAU306PyOjZ6osquixPuTtdSBxgbsdKDcp7Civw3B77BgygVsvx4k3fYr8XCDKChm0uYKScrpFr9_yS6g5mVTQws3KZncZXCQHdupRFoqMS8vXAVgJr52C83AinYWABwH2RYq_B0ZPf_GDzaMgzpep9RlDNecGs57_4zlyxmP2ESU8kjfX8jAA6rYFKeGXJHMD-j4SassoYIzYRv9YkHx8F8Y2ae5Kd5M24Ql0kkvqc_4eO_T9s4nbQ4q5qGHGE-91ND1KVn2avcUyVVPc0-XCR7EH8HnHgCl0v1c7gX1RL7ET7NJbPzfmzQAzk0ZW0dEHI4KZnXSpqy8m-3zAc8kIARm2QwoNEWpy3MWiooPeZVSa9d5iw1aLrbyumfjBP0vCQEPes-Aa6PrARwd5jR9SacO5By0-4emzskvJYRZqbfJ9tXSXDMcAFOAm6kqRPJaj8AO4CyajTC_Lt32_0OLeXqYgNpt3HDqLqGjrb-8fVeQc-hKh0aES8XehQqXj4jMwfsTyj5alsXZm08LwzcFlfQZ7s1kUtmr0_BBNJYcdZUdlu6Qio3LFSRYXNuu6edAO1VH5GKqZISvE1uvDZb2E0Z-rtH-oPp1iSpfvsX47jKJ42LVpI6OahEBri44dzHOIwwm3CIuV8gFzOwR0k"
  ]
}
```

And the decoded JWT payload has the following form:

```
{
  "files": [
    {
      "name": ".manifest",
      "hash": "18745de72c01e80cd9f520f418c009ac4d2c3d02b17a50e2d042158bc352a32d",
      "algorithm": "SHA-256"
    },
    {
      "name": "policy.rego",
      "hash": "d0f242aae34db54ef566bea4d45fcf19700c5d8f87a9db932dfad6d1f0f291ac",
      "algorithm": "SHA-256"
    },
    {
      "name": "bar/data.json",
      "hash": "93a23971a914e5eacbf0a8d25154cda309c3c1c72fbb9914d47c60f3cb681588",
      "algorithm": "SHA-256"
    }
  ]
}
```

The “files” field is generated from the files under the directory
path(s) provided to the ‘sign’ command. During bundle signature
verification, EOPA will check each file name (ex.
“foo/bar/data.json”) in the “files” field exists in the actual bundle.
The file content is hashed using SHA256.

To include additional claims in the payload use the –claims-file flag to
provide a JSON file containing optional claims.

For more information on the format of the “.signatures.json” file see
https://www.openpolicyagent.org/docs/latest/management-bundles/#signature-format.

```
eopa sign <path> [<path> [...]] [flags]
```

### Options

```
  -b, --bundle                    load paths as bundle files or root directories
      --claims-file string        set path of JSON file containing optional claims (see: https://www.openpolicyagent.org/docs/latest/management-bundles/#signature-format)
  -h, --help                      help for sign
  -o, --output-file-path string   set the location for the .signatures.json file (default ".")
      --signing-alg string        name of the signing algorithm (default "RS256")
      --signing-key string        set the secret (HMAC) or path of the PEM file containing the private key (RSA and ECDSA)
      --signing-plugin string     name of the plugin to use for signing/verification (see https://www.openpolicyagent.org/docs/latest/management-bundles/#signature-plugin)
```

------------------------------------------------------------------------

## eopa test

Execute Rego test cases

### Synopsis

Execute Rego test cases.

The ‘test’ command takes a file or directory path as input and executes
all test cases discovered in matching files. Test cases are rules whose
names have the prefix “test\_”.

If the ‘–bundle’ option is specified the paths will be treated as policy
bundles and loaded following standard bundle conventions. The path can
be a compressed archive file or a directory which will be treated as a
bundle. Without the ‘–bundle’ flag OPA will recursively load ALL *.rego,
*.json, and \*.yaml files for evaluating the test cases.

Test cases under development may be prefixed “todo\_” in order to skip
their execution, while still getting marked as skipped in the test
results.

Example policy (example/authz.rego):

```
package authz

allow if {
    input.path == ["users"]
    input.method == "POST"
}

allow if {
    input.path == ["users", input.user_id]
    input.method == "GET"
}
```

Example test (example/authz_test.rego):

```
package authz_test

import data.authz.allow

test_post_allowed if {
    allow with input as {"path": ["users"], "method": "POST"}
}

test_get_denied if {
    not allow with input as {"path": ["users"], "method": "GET"}
}

test_get_user_allowed if {
    allow with input as {"path": ["users", "bob"], "method": "GET", "user_id": "bob"}
}

test_get_another_user_denied if {
    not allow with input as {"path": ["users", "bob"], "method": "GET", "user_id": "alice"}
}

todo_test_user_allowed_http_client_data if {
    false # Remember to test this later!
}
```

Example test run:

```
$ eopa test ./example/
```

If used with the ‘–bench’ option then tests will be benchmarked.

Example benchmark run:

```
$  eopa test --bench ./example/
```

The optional “gobench” output format conforms to the Go Benchmark Data
Format.

The –watch flag can be used to monitor policy and data file-system
changes. When a change is detected, EOPA reloads the policy
and data and then re-runs the tests. Watching individual files (rather
than directories) is generally not recommended as some updates might
cause them to be dropped by OPA.

```
eopa test <path> [path [...]] [flags]
```

### Options

```
      --bench                              benchmark the unit tests
      --benchmem                           report memory allocations with benchmark results (default true)
  -b, --bundle                             load paths as bundle files or root directories
      --capabilities string                set capabilities version or capabilities.json file path
      --count int                          number of times to repeat each test (default 1)
  -c, --coverage                           report coverage (overrides debug tracing)
  -z, --exit-zero-on-skipped               skipped tests return status 0
      --explain {fails,full,notes,debug}   enable query explanations (default fails)
  -f, --format {pretty,json,gobench}       set output format (default pretty)
  -h, --help                               help for test
      --ignore strings                     set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
      --license-key string                 Location of file containing EOPA_LICENSE_KEY
      --license-token string               Location of file containing EOPA_LICENSE_TOKEN
      --log-format {json,json-pretty}      set log format (default json)
  -l, --log-level {debug,info,error}       set log level (default info)
  -m, --max-errors int                     set the number of errors to allow before compilation fails early (default 10)
  -p, --parallel int                       the number of tests that can run in parallel, defaulting to the number of CPUs (explicitly set with 0). Benchmarks are always run sequentially. (default 2)
  -r, --run string                         run only test cases matching the regular expression
  -s, --schema string                      set schema file path or directory path
  -t, --target {rego,wasm}                 set the runtime to exercise (default rego)
      --threshold float                    set coverage threshold and exit with non-zero status if coverage is less than threshold %
      --timeout duration                   set test timeout (default 5s, 30s when benchmarking)
      --v0-compatible                      opt-in to OPA features and behaviors prior to the OPA v1.0 release
      --var-values                         show local variable values in test output
  -v, --verbose                            set verbose reporting mode
  -w, --watch                              watch command line files for changes
```

------------------------------------------------------------------------

## eopa test bootstrap

Generate Rego test mocks automatically from Rego files or bundles

```
eopa test bootstrap [flags] entrypoint [...entrypoint]
```

### Examples

```

Automatically generate tests for a Rego bundle, based on the policy code
and top-level rules:

    eopa test bootstrap -d policy/ my/policy/entrypoint

Note: If using a standard Styra DAS bundle structure, the policy entrypoint
should always be 'main/main':

    eopa test bootstrap -d das-policy/ main/main

Note: 'eopa test bootstrap' will look for .styra.yaml in the current
directory, the repository root, and your home directory. To use a different
config file location, pass --styra-config:

    eopa test bootstrap \
      --styra-config ~/.styra-primary.yaml \
      -d das-policy/ \
      main/main

This command will attempt to generate test mocks automatically to exercise
each top-level rule specified. For full test coverage, additional tests
and test cases may be required!

```

### Options

```
  -d, --data strings          set policy or data file(s). Recursively traverses bundle folders. This flag can be repeated.
  -f, --force                 ignore if test files already exist, overwrite existing content on conflict
  -h, --help                  help for bootstrap
      --ignore strings        set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
      --libraries string      where to copy libraries to (default ".styra/include")
      --log-format string     log format (default "text")
      --log-level string      log level (default "info")
      --secret-file string    file to store the secret in
      --styra-config string   Styra DAS config file to use
      --url string            DAS address to connect to (e.g. "https://my-tenant.styra.com")
```

------------------------------------------------------------------------

## eopa test new

Generate Rego test mocks automatically from Rego files or bundles

```
eopa test new [flags] annotation
```

### Examples

```

Add additional generated tests to an 'eopa bootstrap'-generated test file,
selecting the new tests by their annotated name.

For example, given the annotated rule:

    # METADATA
    # custom:
    #   test-bootstrap-name: my-allow-rule
    allow if { ... }

We can add the rule to the test file using the command:

    eopa test new -d policy/ -e my/policy/allow 'my-allow-rule'

Note: If using a standard Styra DAS bundle structure, the policy entrypoint
should always be 'main/main':

    eopa test new -d das-policy/ -e main/main 'my-allow-rule'

Note: 'eopa test new' will look for .styra.yaml in the current
directory, the repository root, and your home directory. To use a different
config file location, pass --styra-config:

    eopa test new \
      --styra-config ~/.styra-primary.yaml \
      -d das-policy/ \
      -e main/main \
      'my-allow-rule'

Remember that for full test coverage, additional tests and test cases may
be required beyond those generated by this command!

```

### Options

```
  -d, --data strings          set policy or data file(s). Recursively traverses bundle folders. This flag can be repeated.
  -e, --entrypoint string     entrypoint rule or package to use for discovering the annotated test
  -h, --help                  help for new
      --ignore strings        set file and directory names to ignore during loading (e.g., '.*' excludes hidden files)
      --libraries string      where to copy libraries to (default ".styra/include")
      --log-format string     log format (default "text")
      --log-level string      log level (default "info")
      --secret-file string    file to store the secret in
      --styra-config string   Styra DAS config file to use
      --url string            DAS address to connect to (e.g. "https://my-tenant.styra.com")
```

------------------------------------------------------------------------

## eopa version

Print the version of EOPA

### Synopsis

Show version and build information for EOPA.

```
eopa version [flags]
```

### Options

```
  -h, --help   help for version
```
