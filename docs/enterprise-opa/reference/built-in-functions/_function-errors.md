<!-- markdownlint-disable MD041 -->
### Errors

By default—and if `raise_error` is `true`—then an error returned will halt policy evaluation.

If `raise_error` is `false`, then the response object contains the error in an `error` key instead of its usual response.

```rego
{
  "error": ...
}
```
