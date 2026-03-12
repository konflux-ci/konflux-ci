---
title: "Installing from Release"
linkTitle: "Installing from Release"
weight: 4
description: "Step-by-step guide for installing Konflux from a pre-built release bundle."
---

This guide covers deploying Konflux on any Kubernetes cluster using the pre-built release bundle.


## Prerequisites

| Tool | Minimum version |
|------|----------------|
| [git](https://git-scm.com/) | v2.46 |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | v1.31.4 |
| [openssl](https://www.openssl.org/) | v3.0.13 |

- `cluster-admin` permissions
- A Kubernetes cluster with the following dependencies installed
  (see [Setup](#setup)):
  - Tekton (or OpenShift Pipelines when using OpenShift)
  - cert-manager
  - trust-manager
  - Kyverno
  - Pipelines-as-Code

## Setup

1. Clone the repository:

```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci
```

2. Deploy the cluster dependencies:

{{< alert color="info" >}}
If you are working with a local Kind cluster, <a href="{{< relref "install-local#setup" >}}">Local Deployment (Kind)</a>
provides a fully automated setup that handles cluster creation and dependency deployment
in a single step.
{{< /alert >}}

```bash
# Generic Kubernetes
SKIP_DEX=true SKIP_INTERNAL_REGISTRY=true SKIP_SMEE=true ./deploy-deps.sh

# OpenShift - use native operators instead of upstream ones
USE_OPENSHIFT_PIPELINES=true USE_OPENSHIFT_CERTMANAGER=true \
SKIP_DEX=true SKIP_INTERNAL_REGISTRY=true SKIP_SMEE=true \
./deploy-deps.sh
```

Alternatively, apply the individual kustomizations under `dependencies/` manually.

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

## Step 4: Create the GitHub App secret

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

## Step 5: Verify the installation

Check the Konflux CR status:

```bash
kubectl get konflux konflux
```

Wait for the Ready condition:

```bash
kubectl wait --for=condition=Ready konflux/konflux --timeout=15m
```

Check all component pods are running:

```bash
kubectl get pods -A | grep -v Running | grep -v Completed
```

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
- [GitHub Application Secrets]({{< relref "github-secrets" >}}) — create a GitHub App and configure webhook delivery
- [Registry Configuration]({{< relref "registry-configuration" >}}) — configure an external container registry for build and release pipelines
- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) — full CR field reference
- [Troubleshooting]({{< relref "../troubleshooting" >}}) — solutions to common installation and runtime issues
- [Examples]({{< relref "../examples" >}}) — sample Konflux CR configurations
