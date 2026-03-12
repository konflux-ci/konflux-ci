---
title: "Installing from OLM"
linkTitle: "Installing from OLM"
weight: 5
description: "Installing the Konflux Operator through the Operator Lifecycle Manager (OLM)."
---

The Konflux Operator is published to the
[community operators catalog](https://github.com/redhat-openshift-ecosystem/community-operators-prod/tree/main/operators/konflux)
and can be installed through OLM on any cluster that has OLM installed.

## Channels

Channels are scoped to a release stream (the `vMAJOR.MINOR` version). For example,
for the `v0.1` stream:

| Channel | Description |
|---------|-------------|
| `stable-v0.1` | Latest stable release for the `v0.1` stream — recommended for production |
| `candidate-v0.1` | Release candidates for the `v0.1` stream — for early testing of upcoming versions |

Substitute the appropriate stream (e.g. `v0.2`) when a newer stream is available.

## Prerequisites

| Tool | Minimum version |
|------|----------------|
| [git](https://git-scm.com/) | v2.46 |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | v1.31.4 |
| [openssl](https://www.openssl.org/) | v3.0.13 |

- `cluster-admin` permissions
- A Kubernetes cluster with [OLM](https://olm.operatorframework.io/) installed and the
  following dependencies (see [Setup](#setup)):
  - Tekton (or OpenShift Pipelines when using OpenShift)
  - cert-manager
  - trust-manager
  - Kyverno
  - Pipelines-as-Code

{{< alert color="info" >}}
OLM is included by default on OpenShift. For vanilla Kubernetes, follow the
<a href="https://olm.operatorframework.io/docs/getting-started/">OLM getting started guide</a>
to install it first.
{{< /alert >}}

## Setup

1. Clone the repository:

```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci
```

2. Deploy the cluster dependencies:

```bash
# Generic Kubernetes
SKIP_DEX=true SKIP_INTERNAL_REGISTRY=true SKIP_SMEE=true ./deploy-deps.sh

# OpenShift - use native operators instead of upstream ones
USE_OPENSHIFT_PIPELINES=true USE_OPENSHIFT_CERTMANAGER=true \
SKIP_DEX=true SKIP_INTERNAL_REGISTRY=true SKIP_SMEE=true \
./deploy-deps.sh
```

Alternatively, apply the individual kustomizations under `dependencies/` manually.

## Install via kubectl

### Step 1: Create the namespace

```bash
kubectl create namespace konflux-operator
```

### Step 2: Create an OperatorGroup

The Konflux Operator requires a cluster-wide OperatorGroup (`targetNamespaces: []`):

```yaml
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: konflux-operator
  namespace: konflux-operator
spec:
  upgradeStrategy: Default
  targetNamespaces: []
```

```bash
kubectl apply -f operatorgroup.yaml
```

### Step 3: Create a Subscription

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: konflux-operator
  namespace: konflux-operator
spec:
  name: konflux-operator
  # channel: stable-v0.1  # omit to use the default channel
  installPlanApproval: Automatic
  source: community-operators
  sourceNamespace: <catalog-namespace>  # openshift-marketplace on OpenShift, olm on vanilla Kubernetes
```

```bash
kubectl apply -f subscription.yaml
```

### Step 4: Verify the installation

Wait for the operator to be ready:

```bash
kubectl wait --for=condition=Available deployment/konflux-operator-controller-manager \
  -n konflux-operator --timeout=300s
```

Check the subscription and install plan status:

```bash
kubectl get subscription konflux-operator -n konflux-operator
kubectl get installplan -n konflux-operator
```

## Install via the OpenShift Web Console

On OpenShift, you can also install through the OperatorHub UI:

1. Navigate to **Operators → OperatorHub**.
2. Search for **Konflux**.
3. Select the **Konflux Operator** and click **Install**.
4. Choose the desired channel (e.g. **`stable-v0.1`**) and set the installation namespace to `konflux-operator`.
5. Click **Install** and wait for the operator to become ready.


## Create and verify the Konflux Custom Resource

See [Applying the Konflux Custom Resource]({{< relref "apply-konflux-cr" >}}) for instructions
on creating a Konflux CR and verifying that all components are ready.
## What's next


- [Onboard a new Application]({{< relref "onboard" >}}) — onboard an application, run builds, tests, and releases
- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) — full CR field reference
- [Troubleshooting]({{< relref "../troubleshooting" >}}) — solutions to common issues
- [Examples]({{< relref "../examples" >}}) — sample Konflux CR configurations
