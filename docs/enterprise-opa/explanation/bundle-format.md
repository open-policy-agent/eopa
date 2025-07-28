---
sidebar_position: 1
sidebar_label: Bundles in Enterprise OPA
title: Bundles in Enterprise OPA
---

# Bundles

Bundles in Enterprise OPA are similar to [OPA bundles](https://www.openpolicyagent.org/docs/management-bundles/) and are the main mechanism for providing policies and data to a Enterprise OPA instance.

To make Enterprise OPA more performant for large data sets, the bundle format used in Enterprise OPA is different from that which is used in OPA. The Enterprise OPA format is based on a binary JSON representation of data which allows Enterprise OPA to process queries using less memory.


## Bundle Service API with Enterprise OPA Bundles

Other than the difference in the bundle format outlined above, Enterprise OPA handles bundles in the same way as OPA. Using the same configuration options as OPA, Enterprise OPA can be configured to download Enterprise OPA bundles using the Bundle Service API.

---

See the following for additional information:

- [How to convert an OPA bundle into an Enterprise OPA bundle](/enterprise-opa/how-to/migrate-from-opa#convert-bundles)
- [Policy Bundle API reference](/enterprise-opa/reference/configuration/policy/bundle-api)
- [OPA Bundles Documentation](https://www.openpolicyagent.org/docs/management-bundles/)
