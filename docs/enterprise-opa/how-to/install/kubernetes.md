---
title: How to deploy Enterprise OPA on Kubernetes
sidebar_label: Deploy on Kubernetes
sidebar_position: 2
---

# Deploy on Kubernetes

This guide gives an outline of how to deploy Enterprise OPA in Kubernetes. There are a number of adjustments you may wish to consider for your own deployment:

- Setting memory and CPU requests for the Enterprise OPA container. These values will depend on your data and throughput requirements.
- Adjustments to the example configuration file included here as a secret to load bundles over the Bundle Service API.
- Creating an Ingress resource to expose the Enterprise OPA API.
- Deploying `kube-mgmt` to load Kubernetes data or policies in `ConfigMap` resources into Enterprise OPA.


## 1. Create a namespace

This guide uses an example namespace named `eopa`. This is optional, but will require updates to the following YAML files.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: eopa
```


## 2. Store the license in a secret

This is a required step. This secret is used in the Enterprise OPA pods to enable them to start.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: enterprise-opa-license
  namespace: eopa
type: Opaque
stringData:
  license: "..." # <-- set license key here
```

:::tip
If you are storing your Enterprise OPA license key in [HashiCorp Vault](/enterprise-opa/reference/configuration/using-secrets/from-hashicorp-vault), you can skip this step and instead use the Enterprise OPA configuration file.
:::


## 3. Create the configuration file

Create a `ConfigMap` for configuration. This will be loaded into the Enterprise OPA pods via a volume mount.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: enterprise-opa-config
  namespace: eopa
data:
  config.yaml: |
    services:
      example:
        url: https://bundles.example.com/

    bundles:
      example:
        service: example
        resource: bundles/example.tar.gz
```

:::caution
If you're providing anything sensitive in your Enterprise OPA configuration—like tokens or private keys—don't place them in the config map directly. Instead, use [HashiCorp Vault](/enterprise-opa/reference/configuration/using-secrets/from-hashicorp-vault), [environment variable substitution](https://www.openpolicyagent.org/docs/configuration/#environment-variable-substitution) or in a file via the `--set-file` [override](https://www.openpolicyagent.org/docs/configuration/#cli-runtime-overrides) for `eopa run`.
:::


## 4. Create the deployment

Finally, we can run Enterprise OPA by creating a Deployment resource.

:::note
This Deployment makes reference the Enterprise OPA image hosted on the GitHub Container Registry. If this is inaccessible from your cluster, you will need to push a copy of the image to a registry that is accessible and update the image name in the Deployment's Pod spec.
:::


```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: enterprise-opa
  namespace: eopa
  name: enterprise-opa
spec:
  replicas: 1
  selector:
    matchLabels:
      app: enterprise-opa
  template:
    metadata:
      labels:
        app: enterprise-opa
      name: enterprise-opa
    spec:
      containers:
      - name: enterprise-opa
        # Update this to the desired version
        image: # docker pull ghcr.io/styrainc/enterprise-opa:$VERSION
        args:
        - "run"
        - "--server"
        - "--addr=0.0.0.0:8181"
        - "--config-file=/etc/config/config.yaml"
        env:
        - name: EOPA_LICENSE_KEY
          valueFrom:
            secretKeyRef:
              name: enterprise-opa-license
              key: license
        volumeMounts:
        - name: config
          mountPath: /etc/config
        readinessProbe:
          httpGet:
            path: /health
            scheme: HTTP
            port: 8181
          initialDelaySeconds: 3
          periodSeconds: 5
        livenessProbe:
          httpGet:
            path: /health
            scheme: HTTP
            port: 8181
          initialDelaySeconds: 3
          periodSeconds: 5
      volumes:
      - name: config
        configMap:
          name: enterprise-opa-config
          items:
          - key: "config.yaml"
            path: "config.yaml"
```


## 5. Access the Enterprise OPA API


### Connecting to the Enterprise OPA API using `kubectl port-forward`

:::note
This method is only really suitable for local testing.
:::

First, run the following command to forward port 8181 on your local machine to the Enterprise OPA API:

```shell-session
# terminal-command
kubectl -n eopa port-forward deployment/enterprise-opa 8181
Forwarding from 127.0.0.1:8181 -> 8181
Forwarding from [::1]:8181 -> 8181
```

Next, in another terminal, run the following command to test the connection:

```json
# terminal-command
curl --silent localhost:8181/v1/data/system/version?pretty=true
{
  "result": {
    "build_commit": "779a6b0b33fcaf1fc47b42728a610dba7dc5dcac",
    "build_hostname": "github.actions.local",
    "build_timestamp": "2023-02-03T22:52:03Z",
    "version": "0.48.0"
  }
}
```


### Connecting to the Enterprise OPA API using a Service & Ingress

This method is more suitable in the following scenarios:

- You want to run Enterprise OPA in production and have other services in the cluster that depend on it.
- When benchmarking Enterprise OPA from within the cluster.

First, create a Service resource. This will give Enterprise OPA a record in the Kubernetes DNS and make it accessible from other pods in the cluster at `enterprise-opa.eopa.svc.cluster.local:8181`.

```yaml
kind: Service
apiVersion: v1
metadata:
  name: enterprise-opa
  namespace: eopa
spec:
  selector:
    app: enterprise-opa
  ports:
  - port: 8181
```

Optionally, create an Ingress resource to allow the Enterprise OPA instances to be accessed from another location.

:::note
You will need to update the `host` field to hostname you wish to use.
:::

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: enterprise-opa
  namespace: eopa
spec:
  rules:
  - host: enterprise-opa.example.com
    http:
      paths:
      - pathType: Prefix
        path: /
        backend:
          service:
            name: enterprise-opa
            port:
              number: 8181
```

Next, in another terminal, run the following command to test the connection:

```json
# terminal-command
curl enterprise-opa.example.com/v1/data/system/version?pretty=true
{
  "result": {
    "build_commit": "779a6b0b33fcaf1fc47b42728a610dba7dc5dcac",
    "build_hostname": "github.actions.local",
    "build_timestamp": "2023-02-03T22:52:03Z",
    "version": "0.48.0"
  }
}
```
