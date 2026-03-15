---
title: "Building and Installing from Source"
linkTitle: "Building and Installing from Source"
weight: 3
description: "Building and running the Konflux Operator from source for development or custom deployments."
---

Building from source is intended for contributors to the Operator or anyone who needs
to run a custom build. There are two modes:

| Mode | When to use |
|------|-------------|
| [Run Operator locally](#run-operator-locally) | Iterative development - Operator runs on your machine, connects to a cluster |
| [Deploy Operator image](#deploy-operator-image) | Test a containerised build in-cluster |

## Prerequisites

| Tool | Minimum version |
|------|----------------|
| [Go](https://go.dev/) | v1.25.0 |
| [podman](https://podman.io/docs/installation) or [docker](https://docs.docker.com/engine/install/) | podman v5.3.1 / docker v27.0.1 *(required only when building an Operator image)* |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | v1.31.4 |
| [make](https://www.gnu.org/software/make/) | — |
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

{{< alert color="info" >}}
All <code>./scripts/</code> commands run from the <strong>repository root</strong>
(<code>konflux-ci/</code>). All <code>make</code> commands run from the
<strong><code>operator/</code> subdirectory</strong>.
{{< /alert >}}

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

## Run Operator locally

Running the Operator locally is the recommended workflow for most development scenarios.
The Operator process runs on your machine and uses your `kubectl` context to connect to
the cluster - no image build required.

### Step 1: Install the CRDs

```bash
cd operator
make install
```

### Step 2: Start the Operator

```bash
make run
```

The Operator connects to your cluster, watches for `Konflux` Custom Resources, and
reconciles them. Keep this terminal open while you work.

### Step 3: Create The Konflux Custom Resource

In a **separate terminal**, create a Konflux instance by applying one of the samples from
`config/samples/` and wait for Konflux to be ready:

```bash
kubectl apply -f config/samples/<one of the sample files>
kubectl wait --for=condition=Ready=True konflux konflux --timeout=10m
```

### Development workflow

- After making code changes, stop the Operator with **Ctrl+C** and restart: `make run`
- No image rebuild or deployment restart is needed
- Run `make help` to see all available targets

## Deploy Operator image

Use this approach when you want to run your custom build as an in-cluster deployment
(e.g. to test Operator-managed upgrades or RBAC behaviour). There are two paths
depending on your setup.

### Path 1: Full Kind deployment using the script

If you are working with a local Kind cluster, `deploy-local.sh` with
`OPERATOR_INSTALL_METHOD=build` handles the entire flow in one step - it builds the
Operator image from your local checkout, loads it into Kind, deploys all dependencies,
and installs the Operator:

```bash
OPERATOR_INSTALL_METHOD=build ./scripts/deploy-local.sh
```

See [Local Deployment (Kind)]({{< relref "install-local" >}}) for setup instructions
and all configuration options.

### Path 2: Manual deployment on an existing cluster

Use this path when you have an existing cluster that already has Konflux's dependencies
deployed — either manually or using the built-in scripts described in [Setup](#setup) —
and you want to deploy only the Operator image.

#### Step 1: Build and push the image

```bash
cd operator
make docker-build docker-push IMG=<your-registry>/konflux-operator:<tag>
```

Make sure you have push access to the registry and that the cluster can pull from it.

#### Step 2: Install the CRDs

```bash
make install
```

#### Step 3: Deploy the Operator

```bash
make deploy IMG=<your-registry>/konflux-operator:<tag>
```

{{< alert color="info" >}}
If you encounter RBAC errors, ensure your user has cluster-admin privileges.
{{< /alert >}}

#### Step 4: Create The Konflux Custom Resource

Apply one of the samples from `config/samples/` and wait for Konflux to be ready:

```bash
kubectl apply -f config/samples/<one of the sample files>
kubectl wait --for=condition=Ready=True konflux konflux --timeout=10m
```

## Uninstall

Remove the Konflux CR:

```bash
kubectl delete -f config/samples/<the sample file you applied>
```

Remove the CRDs:

```bash
make uninstall
```

Undeploy the Operator (in-cluster mode only):

```bash
make undeploy
```

## What's next

- [Onboard a new Application]({{< relref "onboard" >}}) - onboard an application, run builds, tests, and releases
- [Local Deployment (Kind)]({{< relref "install-local" >}}) - full automated Kind setup using `deploy-local.sh`
- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) - full CR field reference
- [Troubleshooting]({{< relref "troubleshooting" >}}) - solutions to common issues
- [Examples]({{< relref "../examples" >}}) - sample Konflux CR configurations
