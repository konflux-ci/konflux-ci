---
title: "Custom CA Bundle"
linkTitle: "Custom CA Bundle"
weight: 4
description: "Mounting a custom CA bundle into the build-service for environments with private registries or corporate proxies."
---

The build-service controller-manager ships with a trusted-ca volume that, by
default, mounts an optional ConfigMap at a non-default extra-file path. This
passive hook does **not** overlay the image's system trust store. The
`trustedCA` field lets you replace the system trust store with a custom PEM
bundle so that the build-service can establish TLS connections to endpoints
signed by additional Certificate Authorities.

## When you need this

Configure `trustedCA` when the build-service must trust CAs that are **not**
in the container image's default trust store. Common examples include:

- **Corporate proxy** — Outbound traffic goes through an intercepting proxy
  whose TLS certificate is signed by an internal CA.
- **Private container registry** — A self-hosted registry uses a certificate
  signed by a non-public CA.
- **Internal git server** — A self-hosted GitHub Enterprise or GitLab instance
  uses a privately signed certificate.

### When you do NOT need this

- **Public endpoints** — If the build-service only connects to public
  registries and git providers with certificates signed by well-known CAs,
  the default image trust store is sufficient.
- **Platform-injected CA bundles** — In some environments, the platform
  automatically injects additional CAs into running pods (e.g. via a
  mutating admission webhook). If that mechanism is already active and
  working, you do not need `trustedCA`.

## Prerequisites

A ConfigMap containing your CA bundle in PEM format must exist in the
`build-service` namespace. You can use any name and key — the examples
below use `custom-ca-bundle` and `ca-bundle.pem`.

{{% alert title="Important" color="warning" %}}
The ConfigMap must contain a **full PEM bundle** — the complete set of CA
certificates the build-service should trust. Setting `trustedCA` replaces
(not appends to) the image's default trust store, so include both your
custom CAs and any public CAs you need.
{{% /alert %}}

## Configure trustedCA

**Via the top-level Konflux CR** (recommended):

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  buildService:
    spec:
      trustedCA:
        name: custom-ca-bundle
        key: ca-bundle.pem
```

**Via the standalone KonfluxBuildService CR:**

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: KonfluxBuildService
metadata:
  name: konflux-build-service
spec:
  trustedCA:
    name: custom-ca-bundle
    key: ca-bundle.pem
```

Once applied, the operator:

1. Re-targets the existing trusted-ca volume to mount the ConfigMap at the
   system trust path: `/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem`.
2. Sets `optional: false` on the volume, so the pod will not start if the
   ConfigMap is missing.

## Removing the custom CA bundle

To revert to the default trust store, remove only the `trustedCA` field.
Leave the rest of `buildService.spec` unchanged.

```yaml
spec:
  buildService:
    spec:
      # existing fields (replicas, pipelineConfig, webhookURLs, …) stay as they are
      # trustedCA omitted
```

Or remove the field with a targeted patch:

```bash
kubectl patch konflux konflux --type json \
  -p '[{"op": "remove", "path": "/spec/buildService/spec/trustedCA"}]'
```

The operator restores the original extra-file mount and sets the volume
back to `optional: true`.

## Behavior on errors

| Scenario | Behavior |
|---|---|
| ConfigMap does not exist | Pod stays `Pending` with a standard Kubernetes event (`configmap "…" not found`). The operator reconciliation completes normally. |
| ConfigMap created after deploy | Kubernetes schedules the pod once the ConfigMap appears — no operator action needed. |
| ConfigMap deleted after successful deploy | The pod continues running with the previously mounted data. New pods will not start until the ConfigMap is recreated. |

## Updating the CA bundle

To update the CA bundle contents, update the ConfigMap data:

```bash
kubectl -n build-service create configmap custom-ca-bundle \
  --from-file=ca-bundle.pem=/path/to/updated-ca-bundle.pem \
  --dry-run=client -o yaml | kubectl apply -f -
```

Kubernetes automatically propagates ConfigMap data changes to mounted volumes
(typically within ~60 seconds). However, the build-service process caches
the CA trust store in memory at startup, so **a pod restart is required**
for the updated bundle to take effect. Delete the pod and let the Deployment
recreate it:

```bash
kubectl -n build-service rollout restart deployment build-service-controller-manager
```
