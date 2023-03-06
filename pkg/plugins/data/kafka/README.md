# Kafka Plugins

Kafka is a reliable message queue system.
See #3 for steps to setup AWS MSK.

## Running a data plugin

```shell
load --config-file kafka.yaml run -s -l debug testdata/transform.rego
```

```yaml
# kafka.yaml: kafka plugin sample MSK configuration file
plugins:
  data: 
    kafka.messages:
      type: kafka
      urls: ["b-1-public...us-east-1.amazonaws.com:9196","b-2-public...us-east-1.amazonaws.com:9196"]
      topics: [styra-topic]
      rego_transform: "data.e2e.transform"
      tls_ca_cert: rootca.pem
      sasl_mechanism: scram-sha-512
      sasl_username: <user>
      sasl_password: <secret>
```

```rego
# transform.rego
package e2e

import future.keywords.contains
import future.keywords.if

transform contains {"op": "add", "path": payload.id, "value": val} if {
    input.topic == "styra-topic"

    payload := json.unmarshal(base64.decode(input.value))
    val := object.filter(payload, ["name", "roles"])
}
```

AWS MSK Producer script: See #2 on how to setup a terminal an ec2 instance that can talk to MSK.
```shell
./bin/kafka-console-producer.sh --bootstrap-server b-2....kafka.us-east-1.amazonaws.com:9096 --producer.config client_sasl.properties --topic styra-topic
```

Sample messages:
```json
{"id": "d9eccc5c", "name": "Alice", "roles": ["developer", "reports-reader"]}
{"id": "5c0ba07e", "name": "Bob", "roles": ["reports-admin"]}
{"id": "413adc7a", "name": "Eve", "roles": ["database-reader", "database-writer"]}
```

Validate results:
```
curl -s localhost:8181/v1/data/kafka/messages
{"result":{"413adc7a":{"name":"Eve","roles":["database-reader","database-writer"]},"5c0ba07e":{"name":"Bob","roles":["reports-admin"]},"d9eccc5c":{"name":"Alice","roles":["developer","reports-reader"]}}}
```
# Links

1. https://docs.styra.com/load/configuration/data/kafka-streams-api
1. https://docs.aws.amazon.com/msk/latest/developerguide/getting-started.html
1. https://docs.styra.com/das/policies/cloud-storage-management/kafka-platform#secure-kafka-platform-access
