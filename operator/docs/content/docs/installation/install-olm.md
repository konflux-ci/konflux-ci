---
title: "Installing via OLM"
linkTitle: "Installing via OLM"
weight: 4
description: "Installing the Konflux Operator through the Operator Lifecycle Manager (OLM)."
---

The Konflux Operator is published to the
[OpenShift community operators catalog](https://github.com/redhat-openshift-ecosystem/community-operators-prod/tree/main/operators/konflux)
and can be installed through OLM on OpenShift clusters.

## Channels

Channels are scoped to a release stream (the `vMAJOR.MINOR` version). For example,
for the `v0.1` stream:

| Channel | Description |
|---------|-------------|
| `stable-v0.1` | Latest stable release for the `v0.1` stream — recommended for production |
| `candidate-v0.1` | Release candidates for the `v0.1` stream — for early testing of upcoming versions |

Substitute the appropriate stream (e.g. `v0.2`) when a newer stream is available.

## Prerequisites

- OpenShift v4.20 or newer
- `cluster-admin` permissions
- [cert-manager Operator](https://docs.openshift.com/container-platform/latest/security/cert_manager_operator/cert-manager-operator-install.html) installed
- [OpenShift Pipelines Operator](https://docs.redhat.com/en/documentation/red_hat_openshift_pipelines/latest/html/installing_and_configuring/installing-pipelines) installed

OLM is included by default on OpenShift. The operator is published to the OpenShift
community operators catalog and currently targets OpenShift **v4.20 and v4.21**.

## Install via the OpenShift Web Console

1. Open the OpenShift Web Console and navigate to **Operators → OperatorHub**.
2. Search for **Konflux**.
3. Select the **Konflux Operator** and click **Install**.
4. Choose the desired channel (e.g. **`stable-v0.1`**) and set the installation namespace to `konflux-operator`.
5. Click **Install** and wait for the operator to become ready.

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
  sourceNamespace: openshift-marketplace
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

## What's next

Once the Operator is running, create a `Konflux` Custom Resource to deploy all Konflux
components. Continue with [Create a Konflux CR]({{< relref "install-kubernetes#step-2-create-a-konflux-cr" >}})
to configure secrets and verify the installation.

- [Installing on Kubernetes]({{< relref "install-kubernetes" >}}) - apply a Konflux CR and configure secrets
- [Onboard a new Application]({{< relref "onboard" >}}) - onboard an application, run builds, tests, and releases
- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) - full CR field reference
- [Troubleshooting]({{< relref "../troubleshooting" >}}) - solutions to common issues
- [Examples]({{< relref "../examples" >}}) - sample Konflux CR configurations
