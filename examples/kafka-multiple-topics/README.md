# Kafka Transform for multiple topics

This is an advanced example Rego transform for consuming messsages on multiple Kafka topics.

Please see [tranform.rego](./transform.rego) for details.

## Testing

Use `eopa test .` (or `opa test .`) for running [the included unit tests](./transform_test.rego) against the transform policy.

## E2E Testing

In this directory, you should find:

-   A `docker-compose.yaml` that lets you spin up a local RedPanda instance with a handy web console on http://127.0.0.1:8080.
-   A `benthos-kafka.yml` config that lets you use [Benthos](https://benthos.dev) to generate Kafka messages the three topics.
-   An `enterprise-opa.yml` config that lets you spin up an Enterprise OPA instances configured to consume the three topics.

These are the steps, to be run in separate terminal panes or via [mprocs](https://github.com/pvolok/mprocs):

1. `docker compose up`
2. `benthos -c benthos-kafka.yml`
3. `eopa run -ldebug -s transform.rego -c enterprise-opa.yml`

If you run these via `mprocs`, please note that Benthos might warn about not being able to reach Kafka, but it'll retry until it eventually stops after having generated 100k messages with

```
INFO Pipeline has terminated. Shutting down the service  @service=benthos
```

## References:

[See here for more information about the Styra Enterprise OPA Kafka integration](https://docs.styra.com/enterprise-opa/reference/configuration/data/kafka)
