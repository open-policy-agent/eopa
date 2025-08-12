---
sidebar_position: 9
sidebar_label: Pulsar
title: Pulsar Datasource Configuration | EOPA
---

# Pulsar Datasource Configuration

EOPA's support for Apache Pulsar makes it possible to stream data updates to EOPA. This can be useful when events representing changes to data used in policy evaluation are available on a Pulsar topic (CDC, change data capture).


## Example Configuration

The Pulsar integration is provided through the `data` plugin, and needs to be enabled in EOPA's configuration.


### Minimal

```yaml
# eopa-conf.yaml
plugins:
  data:
    users:
      type: pulsar
      url: pulsar://pulsar.corp.com:6650
      topics:
      - users
      rego_transform: "data.pulsar.transform"
```

In addition to the minimal configuration above, note the following:

- The `users` key will be used as a "namespace" by the plug-in, and will have EOPA use `data.users` for data ingested through Pulsar. Use the name makes the most sense for your application.
- The `topics` array will be the Pulsar topics from which EOPA uses consume the messages.
- The `rego_transform` attribute allows using a message transformer on incoming message batches.


### Subscription Name, Type and Initial Position

- The `subscription_name` lets you control the name of the Pulsar subscription. It defaults to `eopa_<INSTANCE_ID>_<MOUNT_POINT>`. For example, our example above would use the subscription name `eopa_<INSTANCE_ID>_users`. `<INSTANCE_ID>` is a UUID that changes with each startup of an EOPA instance.

- The `subscription_type` configurable lets you control the subscription type used with Pulsar.
Valid values are `exclusive` (default), `shared`, `key_shared` and `failover`.

- The `subscription_initial_position` configurable determines where the data plugin starts receiving messages from.
Valid values are `earliest` (default) and `latest`: `earliest` makes the plugin consume all available messages, `latest` will consume messages published after it has subscribed.

The default values for these three configuration options ensure that **each instance** of EOPA that's consuming a Pulsar topic will get **all the messages**.
If this is not desired for your use case, you can tweak the settings.


## Authentication

The Pulsar plugin supports two authentication modes: **Token** and **OAuth2**


### Token Authentication

To use token authentication, set the `auth_token` configurable:

```yaml
plugins:
  data:
    users:
      type: pulsar
      url: pulsar://pulsar.corp.com:6650
      topics:
      - users
      rego_transform: "data.pulsar.transform"
      auth_token: <YOUR_SECRET_TOKEN>
```


### OAuth2 Authentication

To use OAuth2 (client credential flow), set the following configuration options:

```yaml
plugins:
  data:
    users:
      type: pulsar
      url: pulsar://pulsar.corp.com:6650
      topics:
      - users
      rego_transform: "data.pulsar.transform"

      issuer_url: https://your-issuer.corp.com
      client_id: <CLIENT_ID>
      client_secret: <CLIENT_SECRET>
      audience: "pulsar-aud" # optional
      scope: "some-scope" # optional, some OAuth2 servers require it
```


## Message Transformers

The `rego_transform` attribute specifies the path to a rule used to transform incoming messages via `input.incoming` into a format suitable for storage in EOPA. The raw input provided for each transform should be familiar to most Pulsar users:

```json
{
  "id": "<message_id>"
  "key": "<message_key>",
  "producer": "<producer_name>",
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

The mechanism for transforming messages from batches into EOPA's storage is the same for all data plugins.
Since Pulsar is quite similar to Kafka, its [Notes on Transforms](kafka/#notes-on-transforms) apply to Pulsar, too.
