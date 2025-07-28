---
sidebar_position: 1
sidebar_label: Locally using Styra DAS libraries
title: How to develop and test policies locally using Styra DAS libraries
---

:::tip
You **do not need** an Enterprise OPA license to use this functionality.
:::


# How to develop and test policies locally using Styra DAS libraries

This guide shows you how to use the Enterprise OPA CLI to locally develop and test policies that use libraries from Styra DAS.

1. [Install the Enterprise OPA CLI](#install-the-enterprise-opa-cli)
1. [Run `eopa login` to connect to your Styra DAS instance](#eopa-login)
1. [Run `eopa pull` to download the available libraries and library data from Styra DAS](#eopa-pull)
1. [Run `eopa run` to start up a server](#eopa-run)
1. [Run `eopa test` to run unit tests](#eopa-test)

---


## Install the Enterprise OPA CLI

```sh
# terminal-command
brew install styrainc/packages/eopa
```

See the [installation reference guide](/enterprise-opa/how-to/install) for alternatives.

---


## `eopa login`

First, navigate to the directory containing your policy code.

Next, run `eopa login` with the URL of your Styra DAS tenant.

```shell-session
# terminal-command
cd my_project
# terminal-command
eopa login --url https://mytenant.styra.com
```

Your browser should automatically open the login page to your tenant. After signing in, you should be logged in on the command line.

```shell-session
# terminal-command
eopa login --url https://mytenant.styra.com
[INFO] Opening https://mytenant.styra.com/cli-sign-in?callback=57009

[INFO] Successfully logged in
```
:::info
The callback port is randomized.
:::

If the automatic token transfer fails, the browser tab will show you the
token to use. Use the token with the `--read-token` flag to store it manually.

```sh
# terminal-command
eopa login --read-token jlXPF81IAAQWHod1oGjHZNcuWgGKWJYu9gP638dTfwe5UokU7XlGeWGm38m8SWCU460OKiPq-8w=
```

You should see auto-generated `.styra.yaml` and `.styra-session` files.

Refer to the [`eopa login` CLI reference](/enterprise-opa/reference/cli-reference#eopa-login) for a full list of options

---


## `eopa pull`

Now that you are logged in, pull the libraries and library data from your Styra DAS instance.
You should see information about the libraries that were pulled.

```shell-session
# terminal-command
eopa pull
[INFO] Retrieving 3 libraries: banking_lib, custom_snippets, terraform
```

The downloaded libraries will be contained in a `.styra/include` folder

```shell-session
# terminal-command
ls .styra/include/libraries
banking_lib     custom_snippets   terraform
```


Refer to the [`eopa pull` CLI reference](/enterprise-opa/reference/cli-reference#eopa-pull) for a full list of options

---


## `eopa run`

:::note
When developing your policies you can reference your libraries via `data.libraries.<library_name>`, e.g. `import data.libraries.custom_snippets`
:::

After developing your policies (e.g. in the `/policy` folder), running `eopa run` will automatically load in the contents of the `.styra` folder.

```json
# terminal-command
eopa run -s ./policy
{"level":"warning","msg":"no license provided\n\nSign up for a free trial now by running `eopa license trial`\n\nIf you already have a license:\n    Define either \"EOPA_LICENSE_KEY\" or \"EOPA_LICENSE_TOKEN\" in your environment\n        - or -\n    Provide the `--license-key` or `--license-token` flag when running a command\n\nFor more information on licensing Enterprise OPA visit https://docs.styra.com/enterprise-opa/installation/licensing","time":"2024-02-15T13:48:54-08:00"}
{"level":"warning","msg":"Switching to OPA mode. Enterprise OPA functionality will be disabled.","time":"2024-02-15T13:48:54-08:00"}
{"addrs":[":8181"],"diagnostic-addrs":[],"level":"info","msg":"Initializing server. OPA is running on a public (0.0.0.0) network interface. Unless you intend to expose OPA outside of the host, binding to the localhost interface (--addr localhost:8181) is recommended. See https://www.openpolicyagent.org/docs/security/#interface-binding","time":"2024-02-15T13:48:54-08:00"}
```

Refer to the [`eopa run` CLI reference](/enterprise-opa/reference/cli-reference#eopa-login) for a full list of options

:::tip
The Enterprise OPA CLI also supports using locally downloaded libraries during `eopa eval`. Refer to the [`eopa eval` CLI reference](/enterprise-opa/reference/cli-reference#eopa-eval) for usage.
:::

---


## `eopa test`

After developing tests, running `eopa test` will automatically load in the contents of the `.styra` folder

```shell-session
# terminal-command
eopa test ./policy
PASS: 2/2
```

Refer to the [`eopa test` CLI reference](/enterprise-opa/reference/cli-reference#eopa-test) for a full list of options
