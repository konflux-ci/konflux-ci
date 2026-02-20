# Konflux Operator Deployment Guide

This guide covers deploying Konflux using the operator-based approach on any Kubernetes cluster.

## Table of Contents

<!-- toc -->

- [Overview](#overview)
- [Quick Start (Local Development)](#quick-start-local-development)
  * [Prerequisites](#prerequisites)
  * [Setup](#setup)
  * [What Gets Deployed](#what-gets-deployed)
- [Production Deployment](#production-deployment)
  * [Prerequisites](#prerequisites-1)
  * [Step 1: Deploy the Operator](#step-1-deploy-the-operator)
  * [Step 2: Create Konflux CR](#step-2-create-konflux-cr)
  * [Step 3: Apply Configuration](#step-3-apply-configuration)
  * [Step 4: Create Secrets](#step-4-create-secrets)
  * [Step 5: Verify Deployment](#step-5-verify-deployment)
- [Resource Sizing](#resource-sizing)
- [Production Considerations](#production-considerations)
- [Troubleshooting](#troubleshooting)
  * [Operator Not Starting](#operator-not-starting)
  * [Components Not Deploying](#components-not-deploying)
  * [Port 5000 Conflict (macOS)](#port-5000-conflict-macos)
  * [Insufficient Memory (Kind)](#insufficient-memory-kind)
  * [Dex Not Starting](#dex-not-starting)
  * [Secrets Not Found](#secrets-not-found)
- [Related Documentation](#related-documentation)

<!-- tocstop -->

## Overview

Konflux can be deployed on any Kubernetes cluster using the Konflux operator. The operator manages the lifecycle of all Konflux components.

**Two Deployment Approaches:**

1. **Local Development (Kind)**: Automated setup using convenience scripts
2. **Production (Any Cluster)**: Manual operator deployment with custom configuration

## Quick Start (Local Development)

For local development on Kind clusters (macOS or Linux).

### Prerequisites

- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [kustomize](https://kubectl.docs.kubernetes.io/installation/kustomize/)
- [podman](https://podman.io/docs/installation) or [docker](https://docs.docker.com/engine/install/)
- GitHub App with private key

### Setup

The deployment script requires environment variables for GitHub App integration. To assist with setting these, you may copy the template and configure your values as `scripts/deploy-local.env` will be sourced by default if present:

```bash
cp scripts/deploy-local.env.template scripts/deploy-local.env
# Edit scripts/deploy-local.env with your secrets
./scripts/deploy-local.sh
```

See `scripts/deploy-local.env.template` for all available configuration options.

After this, you should be able to open https://localhost:9443 and log in with:
  - Username: `user1@konflux.dev`
  - Password: `password`

### What Gets Deployed

- Kind cluster with proper resource allocation
- Konflux operator
- All Konflux components (UI, build-service, integration-service, etc.)
- Internal OCI registry (accessible at localhost:5001)
- Demo users for local authentication (user1@konflux.dev / password)

## Production Deployment

For deploying Konflux on real Kubernetes clusters (OpenShift, EKS, GKE, etc.).

### Prerequisites

- Kubernetes cluster (1.28+)
- kubectl configured for cluster access
- Cluster admin permissions

### Step 1: Deploy the Operator

Install the operator from the latest release:

```bash
kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/latest/download/install.yaml
```

This installs:
- All Custom Resource Definitions (CRDs)
- Operator deployment and RBAC
- Required namespaces and service accounts

### Step 2: Create Konflux CR

Create a Konflux Custom Resource to deploy all components.

For production deployments, create your own `konflux.yaml` based on the available samples:

**Sample configurations:**
- [konflux-with-github-auth.yaml](../operator/config/samples/konflux-with-github-auth.yaml) - Production example with GitHub OIDC authentication
- [konflux-empty-cr.yaml](../operator/config/samples/konflux-empty-cr.yaml) - Minimal configuration using defaults
- [Sample README](../operator/config/samples/README.md) - Complete documentation of all samples

**Important:** Do not use `konflux_v1alpha1_konflux_demo.yaml` for production - it contains demo users with static passwords for testing only. Configure OIDC authentication instead (see Authentication section below) using the safe default `konflux_v1alpha1_konflux.yaml`.

### Step 3: Apply Configuration

```bash
kubectl apply -f konflux.yaml
```

### Step 4: Create Secrets

Create GitHub integration secrets:

```bash
for ns in pipelines-as-code build-service integration-service; do
  kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
    --from-file=github-private-key=/path/to/github-app.pem \
    --from-literal=github-application-id="123456" \
    --from-literal=webhook.secret="your-webhook-secret"
done
```

For image-controller (if enabled):

```bash
kubectl -n image-controller create secret generic quaytoken \
  --from-literal=quaytoken="your-quay-token" \
  --from-literal=organization="your-quay-organization"
```

### Step 5: Verify Deployment

Check Konflux status:

```bash
kubectl get konflux konflux -o yaml
```

Check component pods:

```bash
kubectl get pods -A | grep konflux
```

Wait for Ready condition:

```bash
kubectl wait --for=condition=Ready konflux/konflux --timeout=15m
```

## Resource Sizing

**Local Development (Kind):**
- Minimal replicas (1)
- Reduced resource requests (30m CPU, 128Mi memory)
- Total cluster memory: 8-16GB

**Production:**
- Multiple replicas (2-3) for HA
- Production resource requests (100m+ CPU, 256Mi+ memory)
- Horizontal scaling based on load

## Production Considerations

For authentication configuration and other production considerations including default configuration and user access, see the [configuration samples README](../operator/config/samples/README.md#production-considerations).

## Troubleshooting

### Operator Not Starting

```bash
# Check operator logs
kubectl logs -n konflux-operator deployment/konflux-operator-controller-manager

# Verify CRDs installed
kubectl get crds | grep konflux
```

### Components Not Deploying

```bash
# Check Konflux status
kubectl get konflux konflux -o jsonpath='{.status.conditions}' | jq

# Check operator events
kubectl get events -n konflux-operator --sort-by='.lastTimestamp'
```

### Port 5000 Conflict (macOS)

Port 5000 is often used by macOS AirPlay Receiver.

**Solution 1:** Disable AirPlay Receiver
- System Settings → General → AirDrop & Handoff → AirPlay Receiver → Off

**Solution 2:** Use a different port

```bash
# In scripts/deploy-local.env
REGISTRY_HOST_PORT=5001
```

### Insufficient Memory (Kind)

```bash
# Check Podman machine memory
podman machine inspect | grep Memory

# Increase memory
podman machine init --memory 16384 --cpus 6 --rootful konflux-dev
podman machine start konflux-dev
```

### Dex Not Starting

```bash
# Check Dex logs
kubectl logs -n dex deployment/dex

# Verify Dex configuration
kubectl get configmap dex -n dex -o yaml
```

### Secrets Not Found

Verify secrets exist in the correct namespaces:

```bash
kubectl get secrets -n pipelines-as-code
kubectl get secrets -n build-service
kubectl get secrets -n integration-service
```

Recreate if missing (see Step 4 in Production Deployment).

## Related Documentation

- [Operator Samples](../operator/config/samples/README.md) - Example Konflux CRs
- [Troubleshooting Guide](troubleshooting.md) - Common issues and solutions
- [Konflux Documentation](https://konflux-ci.dev/docs/) - Full Konflux documentation
