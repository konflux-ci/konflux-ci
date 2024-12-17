Configuring Github Application Secrets
===

The process of creating a GitHub application is explained in
[Pipelines-as-Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup).
The same secret described there, should be deployed to the `build-service` and
`integration-service` namespaces as well.

:gear: Repeat the `kubectl create secret` command described there for the
`pipelines-as-code` namespace, also for those namespace:

```bash
kubectl -n pipelines-as-code create secret generic pipelines-as-code-secret \
```

```bash
kubectl -n build-service create secret generic pipelines-as-code-secret \
```

```bash
kubectl -n integration-service create secret generic pipelines-as-code-secret \
```
