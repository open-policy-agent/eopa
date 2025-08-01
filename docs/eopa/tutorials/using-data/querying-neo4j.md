---
sidebar_position: 3
sidebar_label: Querying Neo4J
title: Querying Neo4J | EOPA
---

# Querying Neo4J

EOPA provides the `neo4j.query` [built-in function](/eopa/reference/built-in-functions/neo4j) for querying Neo4J at the time of a policy decision.


## Overview

In the following example, we'll create a simple ABAC-style policy to determine if a user is allowed to access a resources relating to an [imaginary pet store](https://play.openpolicyagent.org/p/0vg9PPfJCP). In the `input` document, we'll expect:

- A `user` performing the action
- An `action` to be performed
- A `resource` on which the action is performed by the user

The `action` may be `read` or `update`. We will allow the user to perform the `read` action if the `resource` refers to a pet that has not been adopted, or if the user is an employee. We'll only allow the `update` action if the user is a senior level employee, having worked at the pet store for at least 8 years.


## Project setup

You'll need to have Docker already installed, set up, and working to follow along.

1. Prepare a secure username and password to use for the database

    ```shell
    # terminal-command
    export NEO4J_USERNAME=neo4j
    # terminal-command
    export NEO4J_PASSWORD=verysecret
    ```

2. Launch the Neo4J server

    ```shell
    # terminal-command
    docker run \
       --restart always \
       --publish=7474:7474 \
       --publish=7687:7687 \
       --env NEO4J_AUTH="$NEO4J_USERNAME/$NEO4J_PASSWORD" \
       neo4j:5.13.0-bullseye
    ```

3. Save the following to `petstore.txt`, we'll use it to populate some sample data into Neo4J during the next step

    ```cypher
    CREATE (p:Pet { id: "dog123", adopted: true, age: 2, breed: "terrier", name: "toto" }) RETURN p;
    CREATE (p:Pet { id: "dog456", adopted: false, age: 3, breed: "german-shepherd", name: "rintintin" }) RETURN p;
    CREATE (p:Pet { id: "dog789", adopted: false, age: 2, breed: "collie", name: "lassie" }) RETURN p;
    CREATE (p:Pet { id: "dog790", adopted: true, age: 7, breed: "beagle", name: "spot" }) RETURN p;
    CREATE (p:Pet { id: "cat123", adopted: false, age: 1, breed: "fictitious", name: "cheshire" }) RETURN p;
    CREATE (p:Pet { id: "cat456", adopted: true, age: 5, breed: "tabby", name: "mittens" }) RETURN p;
    CREATE (p:Pet { id: "cat789", adopted: false, age: 2, breed: "calico", name: "fred" }) RETURN p;
    CREATE (p:Pet { id: "cat790", adopted: true, age: 1, breed: "calico", name: "norbert" }) RETURN p;
    CREATE (p:Pet { id: "cat791", adopted: true, age: 2, breed: "sphinx", name: "wilhelm" }) RETURN p;
    CREATE (p:Person { id: "person123", name: "alice", tenure: 20, title: "owner"}) RETURN p;
    CREATE (p:Person { id: "person456", name: "bob", tenure: 15, title: "employee"}) RETURN p;
    CREATE (p:Person { id: "person789", name: "eve", tenure: 5, title: "employee"}) RETURN p;
    CREATE (p:Person { id: "person790", name: "dave", tenure: 3, title: "customer"}) RETURN p;
    CREATE (p:Person { id: "person791", name: "mike", tenure: 4, title: "customer"}) RETURN p;

    MATCH (o:Person)
    MATCH(p:Pet)
    WHERE p.id="cat123" AND o.id="person791"
    MERGE (o)-[:OWNS]->(p);

    MATCH (o:Person)
    MATCH(p:Pet)
    WHERE p.id="dog790" AND o.id="person790"
    MERGE (o)-[:OWNS]->(p);

    MATCH (o:Person)
    MATCH(p:Pet)
    WHERE p.id="cat456" AND o.id="person789"
    MERGE (o)-[:OWNS]->(p);

    MATCH (o:Person)
    MATCH(p:Pet)
    WHERE p.id="cat790" AND o.id="person789"
    MERGE (o)-[:OWNS]->(p);

    MATCH (o:Person)
    MATCH(p:Pet)
    WHERE p.id="cat791" AND o.id="person789"
    MERGE (o)-[:OWNS]->(p);
    ```

4. While leaving the Neo4J database running, use this Docker command to populate it with the sample data you saved in `petstore.txt` during the previous step

    :::info
    If you see `The client is unauthorized due to authentication failure.`, you may have forgotten to populate the `NEO4J_PASSWORD` and `NEO4J_USERNAME` environment variables before running the Docker command.
    :::

    ```shell
    # terminal-command
    docker run \
       --env NEO4J_ADDRESS="neo4j://localhost:7687" \
       --env NEO4J_PASSWORD="$NEO4J_PASSWORD" \
       --env NEO4J_USERNAME="$NEO4J_USERNAME" \
       --network host \
       -i \
       neo4j:5.13.0-bullseye \
       cypher-shell --non-interactive \
       < ./petstore.txt
    ```


## Querying Neo4J

We can use `eopa eval` to quickly check that we populated data into the database:

```shell
eopa eval -f pretty 'neo4j.query({"uri": "neo4j://localhost:7687", "auth": {"scheme": "basic", "principal": "neo4j", "credentials": "verysecret"}, "query": "MATCH (p: Pet) WHERE p.id=\"cat456\" RETURN p"})'
```

This should return a result like:

```json
{
  "results": [
    {
      "p": {
        "ElementId": "4:effe5068-124c-4c26-ac05-49604bfa4f6a:33",
        "Id": 33,
        "Labels": [
          "Pet"
        ],
        "Props": {
          "adopted": true,
          "age": 5,
          "breed": "tabby",
          "id": "cat456",
          "name": "mittens"
        }
      }
    }
  ]
}
```


## Simple ABAC Policy

```rego
package app.abac

import rego.v1

default allow := false

allow if user_is_owner

allow if {
	user_is_employee
	action_is_read
}

allow if {
	user_is_employee
	user_is_senior
	action_is_update
}

allow if {
	user_is_customer
	action_is_read
	not pet_is_adopted
}

# NOTE: credentials are hard-coded here for example purposes, in a production
# setting you should use EOPA's vault helpers or some other mechanism to
# properly secure any sensitive API tokens or other credentials.

userReq := {
	"uri": "neo4j://localhost:7687",
	"auth": {"scheme": "basic", "principal": "neo4j", "credentials": "verysecret"},
	"query": "MATCH (p: Person) WHERE p.id=$iuser RETURN p",
	"parameters": {"iuser": input.user},
}

user_attributes := neo4j.query(userReq).results[0].p.Props

petReq := {
	"uri": "neo4j://localhost:7687",
	"auth": {"scheme": "basic", "principal": "neo4j", "credentials": "verysecret"},
	"query": "MATCH (p: Pet) WHERE p.id=$iresource RETURN p",
	"parameters": {"iresource": input.resource},
}

pet_attributes := neo4j.query(petReq).results[0].p.Props

user_is_owner if user_attributes.title == "owner"

user_is_employee if user_attributes.title == "employee"

user_is_customer if user_attributes.title == "customer"

user_is_senior if user_attributes.tenure > 8

action_is_read if input.action == "read"

action_is_update if input.action == "update"

pet_is_adopted if pet_attributes.adopted == true
```

Save this policy to `./bundle/policy.rego`. While keeping the neo4j server from earlier in the tutorial running, you can use `eopa eval` to evaluate this policy with different inputs. Consider the following example:

```shell-session
# terminal-command
printf '{"user": "%s", "action": "%s", "resource": "%s"}\n' person123 update dog123 | eopa eval --bundle ./bundle --stdin-input --format pretty 'data.app.abac.allow'
true
# terminal-command
printf '{"user": "%s", "action": "%s", "resource": "%s"}\n' person789 update dog123 | eopa eval --bundle ./bundle --stdin-input --format pretty 'data.app.abac.allow'
false
# terminal-command
printf '{"user": "%s", "action": "%s", "resource": "%s"}\n' person789 read dog123 | eopa eval --bundle ./bundle --stdin-input --format pretty 'data.app.abac.allow'
true
```

You can adjust the `printf` command arguments to change the user, action, and resource passed into the policy, so you can experiment with how the policy will react with different input values.


## Further Reading

- [neo4j builtin docs](/eopa/reference/built-in-functions/neo4j)
