---
sidebar_position: 4
sidebar_label: Querying Redis
title: Querying Redis | Enterprise OPA
---

# Querying Redis

Enterprise OPA provides the `redis.query` [built-in function](/enterprise-opa/reference/built-in-functions/redis) for querying Redis at the time of a policy decision.


## Overview

In the following example, we will create a simple policy which validates toppings for orders at a restaurant. We will use Redis to store the types of toppings allowed for different types of food orders. This will allow us to quickly change the allowed toppings without deploying a new bundle by adjusting the values stored in Redis.

The input document we will expect:

- A `type`, one of `pizza`, `sandwich`, or `salad`
- An array of `toppings`

The policy will output an object with a Boolean `allowed` key, and if the request was denied, it will also include a `reason` field explaining why.


## Project Setup

You'll need to have Docker already installed, set up, and working to follow along.

1. Launch a Redis server

    ```shell
    # terminal-command
    docker run --publish 6379:6379 redis:latest
    ```

2. Open a new terminal and note down the container ID. You can find it by using the command `docker ps`. You may wish to save it in an environment variable for later use:

    ```shell
    # example, your container ID will be different
    # terminal-command
    export REDIS_CONTAINER_ID=6228413ddb17
    ```

3. Populate the Redis database with sample data:

    ```shell
    # terminal-command
    docker exec $REDIS_CONTAINER_ID redis-cli SADD salad_toppings tomato onion carrot radish "bell pepper" olive feta gorgonzola bacon
    # terminal-command
    docker exec $REDIS_CONTAINER_ID redis-cli SADD pizza_toppings onion "bell pepper" olive feta bacon
    # terminal-command
    docker exec $REDIS_CONTAINER_ID redis-cli SADD sandwich_toppings lettuce tomato bacon onion swiss bacon
    ```

4. Create a Rego policy using the `redis.query()` builtin, save it to `./policy.rego`

    ```rego
    package main

    import rego.v1

    addr := "localhost:6379"

    toppings_key := sprintf("%s_toppings", [input.type])

    invalid_toppings[t] if {
    	some t in input.toppings
    	redis.query({"addr": addr, "command": "SISMEMBER", "args": [toppings_key, t]}).results != true
    }

    main := decision if {
    	input.type == ["sandwich", "pizza", "salad"][_]
    	count(invalid_toppings) > 0
    	decision := {
    		"allow": false,
    		"reason": sprintf("order of type '%s' is not allowed to contain toppings: %+v", [input.type, invalid_toppings]),
    	}
    } else := decision if {
    	input.type == ["sandwich", "pizza", "salad"][_]
    	count(invalid_toppings) == 0
    	decision := {"allow": true}
    } else := decision if {
    	decision := {
    		"allow": false,
    		"reason": sprintf("order type '%s' is not allowed", [input.type]),
    	}
    }
    ```

5. Launch an Enterprise OPA server:

    ```shell-session
    # terminal-command
    eopa run -s ./policy.rego
    ```

6. Now, in a new terminal, we can use `curl` to see how this policy will react to different inputs. For example:

    ```json
    # terminal-command
    curl -LSs -X POST --data '{"input": {"type": "pizza", "toppings": ["onion", "bacon"]}}' localhost:8181/v1/data/main/main
    {"result":{"allow":true}}
    # terminal-command
    curl -LSs -X POST --data '{"input": {"type": "pizza", "toppings": ["onion", "bacon", "gorgonzola"]}}' localhost:8181/v1/data/main/main
    {"result":{"allow":false,"reason":"order of type 'pizza' is not allowed to contain toppings: {\"gorgonzola\"}"}}
    ```

7. To demonstrate how we can change the behavior of the policy without loading a new bundle, we'll add `gorgonzola` to the allowed pizza toppings Redis database. Remember to re-export your `REDIS_CONTAINER_ID` if you have changed terminals.

    ```json
    # terminal-command
    docker exec $REDIS_CONTAINER_ID redis-cli SADD pizza_toppings gorgonzola
    1
    # terminal-command
    curl -LSs -X POST --data '{"input": {"type": "pizza", "toppings": ["onion", "bacon", "gorgonzola"]}}' localhost:8181/v1/data/main/main
    {"result":{"allow":true}}
    ```


## Using Redis Authentication

Previously, we used an unauthenticated Redis server, but in many production use cases, you will need to use authentication to connect to Redis. Picking up where we left off in the previous section...

We can add a user with a password to our Redis database using the commands below.

```sh
# terminal-command
docker exec $REDIS_CONTAINER_ID redis-cli config set requirepass supersecret123
# terminal-command
docker exec $REDIS_CONTAINER_ID redis-cli acl setuser tutorial allcommands allkeys on '>letmein!'
```

We can now access Redis using either the password on the default user:

```json
# terminal-command
eopa eval -f pretty 'redis.query({"addr": "localhost:6379", "auth": {"password": "supersecret123"}, "command": "SMEMBERS", "args": ["pizza_toppings"]})'
{
  "results": [
    "onion",
    "bell pepper",
    "olive",
    "feta",
    "bacon",
    "gorgonzola"
  ]
}
```

... or using our `tutorial` user:

```json
# terminal-command
eopa eval -f pretty 'redis.query({"addr": "localhost:6379", "auth": {"username": "tutorial", "password": "letmein!"}, "command": "SMEMBERS", "args": ["pizza_toppings"]})'
{
  "results": [
    "onion",
    "bell pepper",
    "olive",
    "feta",
    "bacon",
    "gorgonzola"
  ]
}
```

We can modify the policy we wrote in the previous section to use this new username and password by changing the line:

```rego
redis.query({"addr": addr, "command": "SISMEMBER", "args": [toppings_key, t]}).results != true
```

to:

```rego
redis.query({"addr": addr, "auth": {"username": "tutorial", "password": "letmein!"}, "command": "SISMEMBER", "args": [toppings_key, t]}).results != true
```

To avoid hard-coding database credentials in your policy, look into the vault helpers discussed in the [built-in documentation](/enterprise-opa/reference/built-in-functions/redis).
