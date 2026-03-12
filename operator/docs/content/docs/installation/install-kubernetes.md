---
title: "Installing on Kubernetes"
linkTitle: "Installing on Kubernetes"
weight: 3
description: "Step-by-step guide for installing Konflux on a production Kubernetes cluster."
---

This guide covers deploying Konflux on a production Kubernetes cluster (OpenShift, EKS,
GKE, or any other conformant cluster) using the pre-built release bundle.

## Prerequisites

- Kubernetes cluster v1.28 or newer
- `kubectl` configured with cluster-admin access
- A **GitHub Application** with a private key — Konflux uses it to receive webhook events
  from GitHub, trigger build pipelines on pull requests, and write pipeline status back
  to the PR. If your cluster is not reachable from the internet, a smee proxy can be
  used to relay webhook events into the cluster. See
  [GitHub Application Secrets]({{< relref "github-secrets" >}}) for instructions on
  creating one.
- An **external container registry** (e.g. Quay.io, Docker Hub, or any OCI-compliant
  registry) — used by build and release pipelines to push and store container images.
  See [Registry Configuration]({{< relref "registry-configuration" >}}) for setup
  instructions.

## Step 1: Install the operator

Apply the latest release bundle. This installs all CRDs, the operator deployment, RBAC,
and required namespaces in a single command:

```bash
kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/latest/download/install.yaml
```

To install a specific version instead of the latest, replace `latest` with the version tag:

```bash
kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/download/v0.0.1/install.yaml
```

Wait for the operator to be ready:

```bash
kubectl wait --for=condition=Available deployment/konflux-operator-controller-manager \
  -n konflux-operator --timeout=300s
```

## Step 2: Create a Konflux CR

The operator deploys all Konflux components from a single `Konflux` Custom Resource.
Create your own `konflux.yaml` based on one of the available samples — see the
[Examples]({{< relref "../examples" >}}) page for all sample configurations.

{{< alert color="warning" >}}
Do <strong>not</strong> use <code>konflux_v1alpha1_konflux.yaml</code> for production — it contains demo users
with static passwords intended for local testing only. Use OIDC authentication instead.
{{< /alert >}}

## Step 3: Apply the configuration

```bash
kubectl apply -f konflux.yaml
```

## Step 4: Create secrets

### GitHub App secret

Create the GitHub App secret in the three namespaces that require it:

```bash
for ns in pipelines-as-code build-service integration-service; do
  kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
    --from-file=github-private-key=/path/to/github-app.pem \
    --from-literal=github-application-id="<your-app-id>" \
    --from-literal=webhook.secret="<your-webhook-secret>"
done
```

See [GitHub Application Secrets]({{< relref "github-secrets" >}}) for the full procedure,
including webhook proxy setup for clusters not reachable from the internet.

### Container registry secret (build pipeline)

For each user namespace, create a push secret so build pipelines can push images to your
registry, then attach it to the pipeline service account:

```bash
# Create the secret (replace $NS with the target namespace, e.g. user-ns1)
kubectl create -n $NS secret generic regcred \
  --from-file=.dockerconfigjson=<path/to/.docker/config.json> \
  --type=kubernetes.io/dockerconfigjson

# Attach it to the build-pipeline service account for your component
kubectl patch -n $NS serviceaccount "build-pipeline-${COMPONENT_NAME}" \
  -p '{"secrets": [{"name": "regcred"}]}'
```

To obtain a `config.json` for Quay.io, Docker Hub, or other registries, see
[Registry Configuration]({{< relref "/docs/guides/registry-configuration#obtaining-registry-credentials" >}}).

### Quay.io auto-provisioning (optional)

To have Konflux automatically create Quay repositories when onboarding components via
the UI, enable the image-controller and provide a Quay token:

```yaml
# In your Konflux CR
spec:
  imageController:
    enabled: true
```

```bash
kubectl -n image-controller create secret generic quaytoken \
  --from-literal=quaytoken="<your-quay-oauth-token>" \
  --from-literal=organization="<your-quay-org>"
```

See [Registry Configuration]({{< relref "/docs/guides/registry-configuration#quayio-auto-provisioning-image-controller" >}})
for instructions on generating a Quay OAuth token.

## Step 5: Verify the installation

Check the Konflux CR status:

```bash
kubectl get konflux konflux -o yaml
```

Wait for the Ready condition:

```bash
kubectl wait --for=condition=Ready konflux/konflux --timeout=15m
```

Check all component pods are running:

```bash
kubectl get pods -A | grep konflux
```

## Resource sizing

| Environment | Replicas | CPU request | Memory request |
|-------------|----------|-------------|----------------|
| Local / dev | 1 | 30m | 128Mi |
| Production | 2–3 (HA) | 100m+ | 256Mi+ |

To tune resource limits for individual components, edit the `spec` of your Konflux CR.
See the [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) for all available fields.

## Uninstall

Remove the Konflux CR and all managed components:

```bash
kubectl delete konflux konflux
```

Remove the operator and CRDs:

```bash
kubectl delete -f https://github.com/konflux-ci/konflux-ci/releases/latest/download/install.yaml
```

## What's next

- [Onboard a new Application]({{< relref "onboard" >}}) — onboard an application, run builds, tests, and releases
- [Registry Configuration]({{< relref "registry-configuration" >}}) — full registry setup reference
- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) — full CR field reference
- [Troubleshooting]({{< relref "troubleshooting" >}}) — solutions to common installation and runtime issues
- [Examples]({{< relref "../examples" >}}) — sample Konflux CR configurations
