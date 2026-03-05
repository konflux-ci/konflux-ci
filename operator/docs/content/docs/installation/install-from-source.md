---
title: "Building and Installing from Source"
linkTitle: "Building and Installing from Source"
weight: 2
description: "Building and running the Konflux operator from source for development or custom deployments."
---

Building from source is intended for contributors to the operator or anyone who needs
to run a custom build. There are two modes:

| Mode | When to use |
|------|-------------|
| [Run locally](#run-locally) | Iterative development — operator runs on your machine, connects to a cluster |
| [Deploy to cluster](#deploy-to-cluster) | Test a containerised build in-cluster |

## Prerequisites

- [Go](https://go.dev/) v1.25.0 or newer
- [Docker](https://www.docker.com/) 17.03+ or [Podman](https://podman.io/) equivalent
- `kubectl` v1.11.3 or newer, configured against a running cluster
- `make`
- Cluster-admin access

Clone the repository:

```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci/operator
```

## Run locally

Running the operator locally is the recommended workflow for most development scenarios.
The operator process runs on your machine and uses your `kubectl` context to connect to
the cluster — no image build required.

Before running the operator, you need a Kubernetes cluster with Konflux's dependencies
deployed (Tekton, cert-manager, secrets, etc.). The easiest way is to use
`deploy-local.sh` with the `none` install method, which sets up Kind and all
dependencies without installing the operator, leaving that to you:

```bash
OPERATOR_INSTALL_METHOD=none ./scripts/deploy-local.sh
```

See [Local Deployment (Kind)]({{< relref "install-local" >}}) for setup instructions
and all configuration options.

### Step 1: Install the CRDs

```bash
make install
```

### Step 2: Start the operator

```bash
make run
```

The operator connects to your cluster, watches for `Konflux` Custom Resources, and
reconciles them. Keep this terminal open while you work.

### Step 3: Create instances

In a **separate terminal**, create a Konflux instance by applying one of the samples from
`config/samples/` and wait for konflux to be ready:

```bash
kubectl apply -f config/samples/<one of the sample files>
kubectl wait --for=condition=Ready=True konflux konflux --timeout=10m
```

### Development workflow

- After making code changes, stop the operator with **Ctrl+C** and restart: `make run`
- No image rebuild or deployment restart is needed
- Run `make help` to see all available targets

## Deploy to cluster

Use this approach when you want to run your custom build as an in-cluster deployment
(e.g. to test operator-managed upgrades or RBAC behaviour).

### Step 1: Build and push the image

```bash
make docker-build docker-push IMG=<your-registry>/konflux-operator:<tag>
```

Make sure you have push access to the registry and that the cluster can pull from it.

### Step 2: Install the CRDs

```bash
make install
```

### Step 3: Deploy the operator

```bash
make deploy IMG=<your-registry>/konflux-operator:<tag>
```

{{< alert color="info" >}}
If you encounter RBAC errors, ensure your user has cluster-admin privileges.
{{< /alert >}}

### Step 4: Create instances

Create instances of your solution. You can apply the samples (examples) from `config/samples/` and wait for Konflux to be ready:

```bash
kubectl apply -f config/samples/<one of the sample files>
```

## Uninstall

Remove Konflux CR instances:

```bash
kubectl delete -f config/samples/<the sample file you applied>
```

Remove the CRDs:

```bash
make uninstall
```

Undeploy the operator (in-cluster mode only):

```bash
make undeploy
```

## What's next

- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) — full CR field reference
- [Installing on Kubernetes]({{< relref "install-kubernetes" >}}) — install from a pre-built release instead
