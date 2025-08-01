---
sidebar_position: 2
sidebar_label: Kafka
title: Kafka Datasource Configuration | EOPA
---

# Kafka Datasource Configuration

EOPA's support for Apache Kafka makes it possible to stream data updates to EOPA. This can be useful when events representing changes to data used in policy evaluation are available on a Kafka topic (CDC, change data capture).


## Example Configuration

The Kafka integration is provided through the `data` plugin, and needs to be enabled in EOPA's configuration.


### Minimal

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

In addition to the minimal configuration above, note the following:

- The `kafka.messages` key will be used as a "namespace" by the plug-in, and will have EOPA use `data.kafka.messages` for data ingested through Kafka. Use the name makes the most sense for your application.
- The `topics` array will be the Kafka topics from which EOPA uses consume the messages.
- The `rego_transform` attribute allows using a message transformer on incoming message batches.
- The `consumer_group` boolean defines if the data plugin should form its own consumer group.


### Consumer Group

Due to the way Kafka works, multiple instances of EOPA cannot form a consumer group: they would each only see a subset of the data. However, it is possible to have each Kafka data plugin register _its own_ consumer group (of one member). The benefit is that you can gather information about its lag through standard Kafka tooling.

Consumer groups will have a name of `eopa_<INSTANCE_ID>_<MOUNT_POINT>`. For example, our example above would use the group name `eopa_<INSTANCE_ID>_kafka.messages`. `<INSTANCE_ID>` is a UUID that changes with each startup of an EOPA instance.


### Consumer Offset

The default configuration will retrieve _all_ messages persisted for the requested topic.

To only consume messages starting from a certain point in time, you can configure
`from` to one of:
- `"start"`: consume all messages (default)
- `"end"`: consume from the end, i.e., all new messages from the point of connecting
- a duration, e.g. `"1h"`, to get all messages starting from those published 1 hour ago.

For example, a configuration that subscribes to messages starting from those published not more
than ten minutes ago, is:
```yaml
plugins:
  data:
    kafka.messages:
      type: kafka
      urls:
      - broker:29092
      topics:
      - users
      from: 10m
      rego_transform: "data.e2e.transform"
```


## Message Transformers

The `rego_transform` attribute specifies the path to a rule used to transform incoming messages via `input.incoming` into a format suitable for storage in EOPA. The raw input provided for each transform should be familiar to most Kafka users:

```json
{
  "headers": "eyJzb21lIjogImhlYWRlciB2YWx1ZSJ9",
  "key": "",
  "timestamp": 1675346581,
  "topic": "users",
  "value": "eyJwYXlpbmciOiAiYXR0ZW50aW9uIn0"
}
```

Most of the attributes are optional (for example, their values may be empty), and the base64-encoded `value` is typically used.

`rego_transform` policies take a batch of one or more incoming messages as input and return the desired state of the data store of EOPA. Policies also have access to the data already stored via `input.previous`.
Policies might perform operations such as:

- Filtering out or target operations for messages from a particular topic
- Select to ingest only certain fields from the message
- Switch between adding or removing data from the data store by implementing their own merge strategy

An example policy which applies a transformation to messages from the `users` topic is shown below:

```rego
package e2e

import rego.v1

_payload(msg) := json.unmarshal(base64.decode(msg.value))

# this collects all IDs of the messages in a batch
batch_ids contains _payload(msg).id if some msg in input.incoming

# incoming messages are parsed and stored under their ID payload field
transform[payload.id] := val if {
	some msg in input.incoming
	msg.topic == "users"

	payload := _payload(msg)
	val := object.filter(payload, ["name", "roles"])
}

# if no new data came in for a certain message, we'll retain the data
# stored previously
transform[key] := val if {
	some key, val in input.previous
	not key in batch_ids
}
```


### Notes on Transforms


#### Batching

Messages are consumed by the Rego transform in batches. When an EOPA instance connects to a Kafka cluster, those batches can become large -- many messages have to be ingested to get up to speed.

It's possible that multiple messages concerning _the same ID_ are in one batch. Due to the way Rego evaluation works, this leads to an "object insert conflict" ([see here for details on this error type](/opa/errors/eval-conflict-error/object-keys-must-be-unique)).

To avoid that, it needs to be accounted for in the transform. One way to do that is to ensure that the messages considered for the `transform` rule are _the most up-to-date ones_.

For example, let's assume our messages on a topic come with an increasing counter as `key`, and that we have multiple of them in a batch, like these:

```json
{
  "key": 10,
  "value": {
    "id": 1,
    "name": "alice"
  }
}
{
  "key": 12,
  "value": {
    "id": 1,
    "name": "Alice"
  }
}
```

The following transform could be used to only select the most recent message for transformation:

```rego
package e2e

import rego.v1

_payload(msg) := json.unmarshal(base64.decode(msg.value))

_key(msg) := json.unmarshal(base64.decode(msg.key))

batch_ids contains id if some id, _ in incoming_latest

transform[payload.id] := val if {
	some msg in incoming_latest
	payload := _payload(msg)
	val := object.filter(payload, ["name"])
}

transform[key] := val if {
	some key, val in input.previous
	not key in batch_ids
}

# `incoming` groups the messages of the batch by their payload ID, into an object
# whose keys are the message keys (we're assuming their are increasing integers here)
incoming[id][key] := msg if {
	some msg in input.incoming
	key := _key(msg)
	id := _payload(msg).id
}

# `incoming_latest` picks the latest message of each group, by picking the maximum index.
incoming_latest[id] := msg if {
	some id, batch in incoming
	ks := [k | some k, _ in batch]
	latest := max(ks)
	msg := batch[latest]
}
```


#### Change Feeds

All transforms need to declare the end result of incorporating the incoming data, by appropriately **transforming and merging** `input.incoming` and `input.previous`.

If your message payload contains "create"/"update"/"delete" events like in a change feed, you can make that work with the Rego transform as follows:

```rego
package e2e

import rego.v1

batch_ids contains id if some id, _ in incoming_latest

transform[payload.id] := val if {
	some msg in incoming_latest
	payload := _payload(msg)
	payload.type != "delete"
	val := object.filter(payload, ["name"])
}

transform[key] := val if {
	some key, val in input.previous
	not key in batch_ids
}

# for `incoming_latest` see snippet above
```

Here, `batch_ids` contains the incoming IDs, and since we

1. ignore previous data (from `input.previous`) for IDs from `batch_ids`, and
2. ignore incoming data if its type is "delete",

we ultimately end up dropping the data of IDs those entries have been marked as deleted on the change feed.


## Further reading

- Please see the tutorial on configuring [EOPA with Kafka](/enterprise-opa/tutorials/using-data/streaming-kafka) for an end to end example.
