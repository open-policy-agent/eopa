# Kubernetes Deployment

Example code for [deployment](https://docs.styra.com/enterprise-opa/installation/deployment) of Styra Enterprise OPA in a Kubernetes cluster.

To run the example code in a local [kind](https://kind.sigs.k8s.io) cluster, follow the steps outlined below.

First, create a new cluster with the provided configuration file.

```shell
kind create cluster --config kind-config.yaml
```

Edit the `manifests.yaml` file and add your license key in the `styra-enterprise-opa-license` secret manifest.

Next, deploy the resource manifests:

```shell
kubectl apply -f manifests.yaml
```

Last, use port-forwarding to verify that Styra Enterprise OPA is up and reachable:

```shell
kubectl -n eopa port-forward deployment/eopa 8181
```

```shell
curl 'localhost:8181/v1/config?pretty=true'
{
  "result": {
    "decision_logs": {
      "console": true
    },
    "default_authorization_decision": "/system/authz/allow",
    "default_decision": "/system/main",
    "labels": {
      "id": "94d0bf5d-bffc-4f36-831c-c45a0f2a9263",
      "version": "0.49.0-8"
    }
  }
}
```
