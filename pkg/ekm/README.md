# EKM - External Key Manager Plugin

The EKM plugin manages passwords with Vault or other external key manager

## Download development server:

```shell
brew tap hashicorp/tap
brew install hashicorp/tap/vault

# start vault 'dev' server (http)
vault server -dev -dev-root-token-id="dev-only-token"

# add a license key to vault secrets
export VAULT_TOKEN=dev-only-token
export VAULT_ADDR='http://127.0.0.1:8200'

vault kv put -mount=secret license key=<key>
vault kv put -mount=secret acmecorp/bearer token=test123 scheme=Bearer
vault kv put -mount=secret acmecorp "url=https://www.acmecorp.com"
vault kv put -mount=secret rsa key=public1
vault kv put -mount=secret tls/bearer token=test123 scheme=Bearer

# test vault logical read
vault read secret/data/license
```

## Run

```shell
load --config-file testdata/ekm.yaml run -s -l debug
```

# Links

https://github.com/StyraInc/load-private/issues/415
