---
sidebar_position: 3
sidebar_label: Querying DynamoDB
title: Querying DynamoDB with dynamodb.get and dynamodb.query | EOPA
---

# Querying DynamoDB with `dynamodb.get` and `dynamodb.query`

EOPA provides the `dynamodb.get` and `dynamodb.query` built-in functions for querying DynamoDB during policy evaluation.

The built-ins currently support the [GetItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_GetItem.html)
and the [Query](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_Query.html) operations.


## Overview

In this tutorial, we'll turn the usual order around and dive right in on using the built-in functions! If you'd like to
run the examples on your own machine later, the instructions for doing so are provided further down in the article.


## Using `dynamodb.get`

The `dynamodb.get` function works similarly to the other database-related built-in functions of EOPA, or the
`http.send` function in OPA/EOPA.

```rego
package servers

# Get a server by ID
response := dynamodb.get({
    "cache": true,
    "cache_duration": "10s",
    "credentials": {
        "access_key": "foo",
        "secret_key": "bar"
    },
    "endpoint": "http://localhost:8000",
    "region": "eu-north-1",
    "table": "Servers",
    "key": {
        "ID": {"S": "d28eef3e-b78a-4f68-a5ed-b30479f27cdb"},
    }
})

is_web_server if response.row.Function == "Web"

# Response would look something like this
# {
#     "row": {
#         "ID": "d28eef3e-b78a-4f68-a5ed-b30479f27cdb",
#         "Name": "Megalodon",
#         "Function": "Storage"
#     }
# }
```


## Project setup

If you'd like to try the example yourself, the following steps will get you started.

<!-- markdownlint-disable MD044 -->
1. For the sake of simplicity, we'll be using the [dynamodb-local](https://hub.docker.com/r/amazon/dynamodb-local) Docker
   image in the examples below. This image is a local version of DynamoDB that can be used for development and testing.

    ```shell
    docker run -p 8000:8000 amazon/dynamodb-local
    ```
<!-- markdownlint-enable MD044 -->

1. In order to not use real credentials for this demo, we'll be using a fake AWS access key and secret key. You can
    provide those using the `~/.aws/credentials` file. Obviously, you'll want to save your real credentials in a backup
    file before doing this, should you have any!

    ```ini
    [default]
    aws_access_key_id = foo
    aws_secret_access_key = bar
    region = eu-north-1
    ```

1. We'll also use the AWS CLI application:

    ```shell
    # terminal-command
    brew install awscli
    ```

    Or any other [download option](https://aws.amazon.com/cli/). Refer to the
    [documentation](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Tools.CLI.html) for more
    information on how to use the AWS CLI tool with DynamoDB.

1. Some data to query. We'll use a list of servers in a DynamoDB table called `Servers`.
   Create a file called `servers.json` with the following contents:

   ```json
   {
     "Servers": [
       {
         "PutRequest": {
           "Item": {
             "ID": {
               "S": "2de2f522-32f2-4d24-bfec-1b7b86ff8944"
             },
             "Name": {
               "S": "T-Rex"
             },
             "Function": {
               "S": "Web"
             }
           }
         }
       },
       {
         "PutRequest": {
           "Item": {
             "ID": {
               "S": "50acb25e-1e9b-4a01-bc99-aa1707e8227a"
             },
             "Name": {
               "S": "Velicraptor"
             },
             "Function": {
               "S": "Web"
             }
           }
         }
       },
       {
         "PutRequest": {
           "Item": {
             "ID": {
               "S": "d28eef3e-b78a-4f68-a5ed-b30479f27cdb"
             },
             "Name": {
               "S": "Megalodon"
             },
             "Function": {
               "S": "Storage"
             }
           }
         }
       },
       {
         "PutRequest": {
           "Item": {
             "ID": {
               "S": "29bc37fe-2b6d-4812-ad6e-6e603c7c9cbc"
             },
             "Name": {
               "S": "Allosaurus"
             },
             "Function": {
               "S": "Database"
             }
           }
         }
       }
     ]
   }
   ```

   We'll keep the list short here, but feel free to populate with as many items as you want.
   Next, let's create a table for our data, containing a primary key index for the `ID` attribute:

   ```shell
   # terminal-command
   aws dynamodb create-table \
       --endpoint-url http://localhost:8000 \
       --table-name Servers \
       --attribute-definitions \
           AttributeName=ID,AttributeType=S \
       --key-schema AttributeName=ID,KeyType=HASH \
       --provisioned-throughput ReadCapacityUnits=1,WriteCapacityUnits=1 \
       --table-class STANDARD
   ```

   With a table in place, we can go ahead an populate it with the data from `servers.json`:

   ```shell
   # terminal-command
   aws dynamodb batch-write-item \
       --endpoint-url http://localhost:8000 \
       --request-items file://servers.json \
       --return-consumed-capacity INDEXES \
       --return-item-collection-metrics SIZE
   ```

You can now use `dynamodb.get` to query the `Servers` table like in the example provided above.

**Tip:** If you're using `eopa eval` to evaluate your policy, the `--strict-builtin-errors` can help catch mistakes
like missing required attributes in the `dynamodb.get` request object. See the example below for its usage.


## Using `dynamodb.query`

What if we wanted to query the `Servers` table for all servers with a specific function rather than one-by-one using
the `ID` key? The `dynamodb.query` built-in can be used to retrieve multiple items from a DynamoDB table. The built-in
requires a `table` containing a global secondary index (GSI). We could either create one at the same time as we create
the table, or we may add one later. Let's see what adding a new index for `Function` looks like. To avoid typing in JSON
in the shell, we'll store our index definition in a file called `index.json`:

```json
[
  {
    "Create": {
      "IndexName": "FunctionIndex",
      "KeySchema": [
        {
          "AttributeName": "Function",
          "KeyType": "HASH"
        }
      ],
      "Projection": {
        "ProjectionType": "ALL"
      },
      "ProvisionedThroughput": {
        "ReadCapacityUnits": 10,
        "WriteCapacityUnits": 5
      }
    }
  }
]
```

With that in place, we may go ahead and update our table to include the new index:

```shell
# terminal-command
aws dynamodb update-table \
    --endpoint-url http://localhost:8000 \
    --table-name Servers \
    --attribute-definitions AttributeName=Function,AttributeType=S  \
    --global-secondary-index-updates file://index.json
```

We're now ready to query our database using the new index. Let's try and get a list of all `Web` servers in the list:

```rego
# dynamodb.rego
package dynamodb

response := dynamodb.query({
	"endpoint": "http://localhost:8000",
	"table": "Servers",
	"index_name": "FunctionIndex",
	"key_condition_expression": "#f = :value",
	"expression_attribute_values": {":value": {"S": "Web"}},
	"expression_attribute_names": {"#f": "Function"},
	"credentials": {"access_key": "foo", "secret_key": "bar"},
	"region": "eu-north-1",
})
```

The response is now a list of items rather than a single one. Using `eopa eval` to query the `response` rule:

```shell
# terminal-command
eopa eval --format pretty --strict-builtin-errors --data dynamodb.rego data.dynamodb.response
```

We should see something like this in the output:

```json
{
  "rows": [
    {
      "Function": "Web",
      "ID": "50acb25e-1e9b-4a01-bc99-aa1707e8227a",
      "Name": "Velicraptor"
    },
    {
      "Function": "Web",
      "ID": "2de2f522-32f2-4d24-bfec-1b7b86ff8944",
      "Name": "T-Rex"
    }
  ]
}
```


## Configuration Reference

To see all options available for the `dynamodb.get` and
`dynamodb.query` built-in functions, refer to the [built-in function
reference](/eopa/reference/built-in-functions/dynamodb).
