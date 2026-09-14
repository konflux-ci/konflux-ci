---
title: "Custom CA Bundle"
linkTitle: "Custom CA Bundle"
weight: 4
description: "Mounting a custom CA bundle into the build-service for environments with private registries or corporate proxies."
---

The build-service controller-manager ships with a trusted-ca volume that
mounts an extra file at `/etc/ssl/certs/ca-custom-bundle.crt`. CAs in that
file are added to the image trust store; the store itself is not replaced.

By default the volume points at ConfigMap `trusted-ca` (key `ca-bundle.crt`)
with `optional: true`. On OpenShift the operator creates that ConfigMap with
`inject-trusted-cabundle` so the platform fills `ca-bundle.crt`.

The `trustedCA` field points that **same extra-file path** at a different
ConfigMap in the `build-service` namespace. The file becomes exactly the
PEM at `spec.trustedCA.key`. It is not merged with ConfigMap `trusted-ca`.
The image trust store is unchanged. On OpenShift the platform-injected
ConfigMap may still exist, but it is no longer mounted.

## When you need this

Configure `trustedCA` when the extra CAs you need are **not** in the
default `trusted-ca` / `ca-bundle.crt` object. Common examples include:

- **trust-manager Bundle** — The Bundle writes a ConfigMap in
  `build-service` that is not named `trusted-ca`, or uses a key other
  than `ca-bundle.crt`.
- **Private registry or other extra CA** — You keep a ConfigMap in
  `build-service` whose PEM is the extra-file you want mounted.

### When you do NOT need this

- **Public endpoints** — If the build-service only connects to public
  registries and git providers with certificates signed by well-known CAs,
  the default image trust store is sufficient.
- **Platform-injected CA bundles** — If OpenShift already injects the CAs
  you need into ConfigMap `trusted-ca`, the baked-in extra-file mount
  already consumes that object. You do not need `trustedCA`.

## Prerequisites

A ConfigMap containing your CA bundle in PEM format must exist in the
`build-service` namespace. You can use any name and key — the examples
below use `custom-ca-bundle` and `ca-bundle.pem`. The key name does not
have to end in `.pem`; it is only the ConfigMap data key. The operator
projects that key onto the extra-file mount as `ca-bundle.crt`.

{{% alert title="Important" color="warning" %}}
`/etc/ssl/certs/ca-custom-bundle.crt` is **replaced** by the referenced
key, not merged with ConfigMap `trusted-ca`. Put every CA that should
appear in that extra file into this ConfigMap (the new CA and any others
you still need there). Public CAs from the container image remain. On
OpenShift, cluster or proxy CAs that lived only in the platform-injected
`trusted-ca` ConfigMap are not mounted while `trustedCA` is set.
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

1. Points the existing trusted-ca volume at that ConfigMap name and key.
   The file is still projected as `ca-bundle.crt` and mounted at
   `/etc/ssl/certs/ca-custom-bundle.crt`.
2. Sets `optional: false` on the volume, so the pod will not start if the
   ConfigMap is missing.
3. Stamps a content hash on the controller-manager pod template so later
   ConfigMap updates roll the Deployment.

## Removing the custom CA bundle

To revert to the default `trusted-ca` ConfigMap, remove only the
`trustedCA` field. Leave the rest of `buildService.spec` unchanged.

```yaml
spec:
  buildService:
    spec:
      # existing fields (replicas, pipelineConfig, webhookURLs, …) stay as they are
      # trustedCA omitted
```

Or clear the field with a merge patch (idempotent if `trustedCA` is already
absent):

```bash
kubectl patch konflux konflux --type=merge -p '
spec:
  buildService:
    spec:
      trustedCA: null
'
```

The operator restores the volume to ConfigMap `trusted-ca` / key
`ca-bundle.crt` and sets `optional: true`. The extra-file mount path does
not change.

## Behavior on errors

| Scenario | Behavior |
|---|---|
| ConfigMap does not exist | Pod stays `Pending` with a standard Kubernetes event (`configmap "…" not found`). The operator reconciliation completes normally. |
| ConfigMap exists but the key is missing | Pod stays `ContainerCreating`. Kubelet reports that the volume references a non-existent config key. The operator reconciliation completes normally. |
| ConfigMap created after deploy | Kubernetes schedules the pod once the ConfigMap appears. The operator watches the ConfigMap and stamps a content hash so the Deployment rolls with the bundle. |
| ConfigMap deleted after successful deploy | The pod continues running with the previously mounted data. New pods will not start until the ConfigMap is recreated. |

## Updating the CA bundle

Update the ConfigMap data:

```bash
kubectl -n build-service create configmap custom-ca-bundle \
  --from-file=ca-bundle.pem=/path/to/updated-ca-bundle.pem \
  --dry-run=client -o yaml | kubectl apply -f -
```

The operator watches that ConfigMap and stamps a content hash on the
controller-manager pod template. Kubernetes then rolls the Deployment so
the process loads the new bundle. You do not need to restart the pod.

`subPath` mounts do not pick up ConfigMap data changes, and the process
loads the CA trust store once at startup. The hash annotation is what
triggers the rollout.
