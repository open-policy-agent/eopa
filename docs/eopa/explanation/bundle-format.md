---
sidebar_position: 1
sidebar_label: Bundles in EOPA
title: Bundles in EOPA
---

# Bundles

Bundles in EOPA are similar to [OPA bundles](https://www.openpolicyagent.org/docs/management-bundles/) and are the main mechanism for providing policies and data to a EOPA instance.

To make EOPA more performant for large data sets, the bundle format used in EOPA is different from that which is used in OPA. The EOPA format is based on a binary JSON representation of data which allows EOPA to process queries using less memory.


## Bundle Service API with EOPA Bundles

Other than the difference in the bundle format outlined above, EOPA handles bundles in the same way as OPA. Using the same configuration options as OPA, EOPA can be configured to download EOPA bundles using the Bundle Service API.

---

See the following for additional information:

- [How to convert an OPA bundle into an EOPA bundle](/eopa/how-to/migrate-from-opa#convert-bundles)
- [Policy Bundle API reference](/eopa/reference/configuration/policy/bundle-api)
- [OPA Bundles Documentation](https://www.openpolicyagent.org/docs/management-bundles/)
