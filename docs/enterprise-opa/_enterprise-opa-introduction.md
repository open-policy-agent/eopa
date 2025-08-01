<!-- markdownlint-disable MD041 -->
EOPA is an enterprise-grade drop-in replacement for the [Open Policy Agent (OPA)](https://www.openpolicyagent.org/) that offers:

- **Datasource integrations**: Connect quickly to your [Kafka](/enterprise-opa/reference/configuration/data/kafka), [LDAP](/enterprise-opa/reference/configuration/data/ldap), DynamoDB, [S3](/enterprise-opa/reference/configuration/data/s3), [SQL database](/enterprise-opa/tutorials/using-data/querying-sql), MongoDB and Vault without needing to write or manage your own plugins.
- **Secrets manager integration**: Connect to HashiCorp Vault to [securely use `http.send`](/enterprise-opa/reference/configuration/using-secrets/from-hashicorp-vault).
- **Logging integrations**: Send your authorization decision logs to [Splunk](/enterprise-opa/reference/configuration/decision-logs/splunk) or [Kafka](/enterprise-opa/reference/configuration/decision-logs/kafka).
- **Live impact analysis**: Check to see if your new policies [impact production](/enterprise-opa/tutorials/testing/live-impact-analysis) _before_ they are merged.
- **Lower costs**: Use cheaper cloud infrastructure because EOPA uses 10x less memory and 40% less CPU than OPA.
