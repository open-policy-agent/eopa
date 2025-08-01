---
sidebar_position: 5
sidebar_label: Streaming data from Kafka
title: Streaming data from Kafka | EOPA
---


# Streaming data from Kafka

<!-- markdownlint-disable MD044 -->
import LicenseTrialAdmonition from '../../../enterprise-opa/_license-trial-admonition.md';


EOPA has the capability to ingest data from Kafka topics and use it in policy evaluation.
This can be useful to keep a dataset that is frequently updated fresh in EOPA.


## Overview

In this tutorial we'll be walking through how to use EOPA's Kafka integration.
To demo the Kafka functionality, we'll need to complete the following:

- Run Kafka broker and EOPA locally in some containers
- Publish some data on the Kafka topic
- Query the data which was streamed to EOPA


## Running the Kafka and EOPA using Docker Compose

EOPA requires a Kafka cluster to test the Kafka integration.
For this tutorial, we'll run a local containerized test "cluster", using the Confluent images for Kafka and Zookeeper.

:::note
If you have a cluster running already, you may skip this step.
In this tutorial, we are running a few containers and are using Docker Compose to orchestrate the procedure.
:::

Create a file called `docker-compose.yaml` and insert the following configuration:

```yaml
# docker-compose.yaml
version: '3'
services:
  zookeeper:
    image: confluentinc/cp-zookeeper:7.3.0
    container_name: zookeeper
    environment:
      ZOOKEEPER_CLIENT_PORT: 2181
      ZOOKEEPER_TICK_TIME: 2000
  broker:
    image: confluentinc/cp-kafka:7.3.0
    container_name: broker
    ports:
      - "9092:9092"
    depends_on:
      - zookeeper
    environment:
      KAFKA_BROKER_ID: 1
      KAFKA_ZOOKEEPER_CONNECT: 'zookeeper:2181'
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: PLAINTEXT:PLAINTEXT,PLAINTEXT_INTERNAL:PLAINTEXT
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://localhost:9092,PLAINTEXT_INTERNAL://broker:29092
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: 1
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: 1
  enterprise-opa:
    image: ghcr.io/styrainc/enterprise-opa:latest
    ports:
      - "8181:8181"
    command:
      - "run"
      - "--server"
      - "--addr=0.0.0.0:8181"
      - "--log-level=debug"
      - "--config-file=/data/enterprise-opa-conf.yaml"
      - "/data/policy/transform.rego"
    environment:
      EOPA_LICENSE_KEY: ${EOPA_LICENSE_KEY}
    volumes:
      - "./:/data"
    depends_on:
      - broker
```

:::warning
The Kafka deployment above uses the settings from the [Kafka quickstart](https://developer.confluent.io/quickstart/kafka-docker/), and is not suitable for production.
This is appropriate for testing purposes only.
:::

The `eopa` configuration mounts the current directory into `/data` in the container. We need this directory to contain a configuration file and a transformation policy file to format data as it is ingested into EOPA.

Create a file called `enterprise-opa-conf.yaml`, this file is used to configure EOPA to ingest data from Kafka.
We must also instruct EOPA on which policy to use to transform the data as it is ingested.
Insert the content below into the `enterprise-opa-conf.yaml` file in the current directory.

```yaml
# enterprise-opa-conf.yaml
plugins:
  data:
    kafka.messages:
      type: kafka
      urls:
        - broker:29092
      topics:
        - users
      rego_transform: "data.e2e.transform"
```

We also need to supply a policy to be loaded at `data.e2e.transform`.
We do this by creating a local file called `transform.rego` in the current directory.
The policy below will ensure that only messages on the `users` topic are ingested, and that the data is transformed to only include the `name` and `roles` fields.
Insert the content below into the `transform.rego` file so that it can be loaded into the EOPA container.

```rego
# transform.rego
package e2e

import rego.v1

_payload(msg) := json.unmarshal(base64.decode(msg.value))

batch_ids contains _payload(msg).id if some msg in input.incoming

transform[payload.id] := val if {
	some msg in input.incoming
	msg.topic == "users"

	payload := _payload(msg)
	val := object.filter(payload, ["name", "roles"])
}

transform[key] := val if {
	some key, val in input.previous
	not key in batch_ids
}
```

Before running EOPA, we need to set the `EOPA_LICENSE_KEY` environment variable.

<LicenseTrialAdmonition />

``` shell
# terminal-command
export EOPA_LICENSE_KEY=<license key here>
```

We can now bring up the demo with the following command:

``` shell
# terminal-command
docker-compose up
```


## Publishing messages to Kafka

With the containers up and running, it's time to test the EOPA integration with Kafka.

:::note
The following example uses the [kcat tool](https://github.com/edenhill/kcat) to produce messages to Kafka topics in these examples, but any tool for producing messages should work.
:::

We're going to publish some messages to the `users` topic, which we previously configured EOPA to consume from.
The data will be in the form of a JSON object per line, and will be stored in a file called `users.jsonl`.

You can create the `users.jsonl` file now with the content below.

```json
{
  "id": "d9eccc5c",
  "name": "Alice",
  "roles": [
    "developer",
    "reports-reader"
  ]
}
{
  "id": "5c0ba07e",
  "name": "Bob",
  "roles": [
    "reports-admin"
  ]
}
{
  "id": "413adc7a",
  "name": "Eve",
  "roles": [
    "database-reader",
    "database-writer"
  ]
}
```
Now we can use this data to publish messages to the `users` topic.

```shell
# terminal-command
kcat -P -b localhost -t users < resources/users.jsonl
```
To verify that the users now are consumable on the `users` topic, we can invoke `kcat` as a consumer.

```json
# terminal-command
kcat -C -b localhost -t users
{"id": "d9eccc5c", "name": "Alice", "roles": ["developer", "reports-reader"]}
{"id": "5c0ba07e", "name": "Bob", "roles": ["reports-admin"]}
{"id": "413adc7a", "name": "Eve", "roles": ["database-reader", "database-writer"]}
% Reached end of topic users [0] at offset 3
```


## Query the data which was streamed to EOPA

The same data we have seen using `kcat` can now be queried from EOPA.

We can run a simple curl command to verify that the data has been ingested and transformed correctly.

```json
# terminal-command
curl -s "localhost:8181/v1/data/kafka/messages?pretty=true"
{
  "result": {
    "413adc7a": {
      "name": "Eve",
      "roles": [
        "database-reader",
        "database-writer"
      ]
    },
    "5c0ba07e": {
      "name": "Bob",
      "roles": [
        "reports-admin"
      ]
    },
    "d9eccc5c": {
      "name": "Alice",
      "roles": [
        "developer",
        "reports-reader"
      ]
    }
  }
}
```


## Further Reading

- The files used in the examples are also available in the EOPA [blueprints repo](https://github.com/StyraInc/enterprise-opa/tree/main/examples/kafka).
- View the [Kafka configuration](/enterprise-opa/reference/configuration/data/kafka) for EOPA.
