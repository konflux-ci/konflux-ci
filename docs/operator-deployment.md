# Konflux Operator Deployment Guide

This guide covers deploying Konflux using the operator-based approach on any Kubernetes cluster.

## Table of Contents

- [Overview](#overview)
- [Quick Start (Local Development)](#quick-start-local-development)
- [Production Deployment](#production-deployment)
- [Configuration](#configuration)
- [Authentication](#authentication)
- [Demo Resources](#demo-resources)
- [Troubleshooting](#troubleshooting)

## Overview

Konflux can be deployed on any Kubernetes cluster using the Konflux operator. The operator manages the lifecycle of all Konflux components.

**Two Deployment Approaches:**

1. **Local Development (Kind)**: Automated setup using convenience scripts
2. **Production (Any Cluster)**: Manual operator deployment with custom configuration

## Quick Start (Local Development)

For local development on Kind clusters (macOS or Linux).

### Prerequisites

- kind: `brew install kind`
- kubectl: `brew install kubectl`
- podman: `brew install podman` (macOS) or docker (Linux)
- GitHub App with private key

### Setup

1. **Create configuration from templates:**

   ```bash
   # Copy Konflux CR template
   cp my-konflux.yaml.template my-konflux.yaml

   # Copy environment template
   cp scripts/deploy-local-dev.env.template scripts/deploy-local-dev.env
   ```

2. **Edit `scripts/deploy-local-dev.env` with your secrets:**

   ```bash
   GITHUB_PRIVATE_KEY_PATH="/path/to/your/github-app.pem"
   GITHUB_APP_ID="123456"
   WEBHOOK_SECRET="your-webhook-secret"
   QUAY_TOKEN=""  # Optional - only for image-controller
   ```

3. **Deploy:**

   ```bash
   ./scripts/deploy-local-dev.sh my-konflux.yaml
   ```

4. **Access Konflux:**

   Open https://localhost:9443

### What Gets Deployed

- Kind cluster with proper resource allocation
- Konflux operator
- All Konflux components (UI, build-service, integration-service, etc.)
- Internal OCI registry (accessible at localhost:5001)
- **No demo users by default** (secure default)

### Optional: Demo Resources

Demo users are disabled by default for security. To enable for testing:

```bash
# In scripts/deploy-local-dev.env
DEPLOY_DEMO_RESOURCES=1
```

Or deploy separately:

```bash
./scripts/deploy-demo-resources.sh
```

Demo credentials: `user1@konflux.dev` / `password`

## Production Deployment

For deploying Konflux on real Kubernetes clusters (OpenShift, EKS, GKE, etc.).

### Prerequisites

- Kubernetes cluster (1.28+)
- kubectl configured for cluster access
- Cluster admin permissions

### Step 1: Deploy the Operator

```bash
cd operator

# Install CRDs
make install

# Build and push operator image (or use released image)
make docker-build docker-push IMG=<your-registry>/konflux-operator:tag

# Deploy operator
make deploy IMG=<your-registry>/konflux-operator:tag
```

### Step 2: Create Konflux CR

Create a `konflux.yaml` file:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  # Enable components as needed
  ui:
    spec:
      ingress:
        enabled: true
        ingressClassName: "nginx"
        host: "konflux.example.com"
        annotations:
          cert-manager.io/cluster-issuer: "letsencrypt-prod"
        tlsSecretName: "konflux-ui-tls"
      proxy:
        replicas: 3

  integrationService:
    spec:
      integrationControllerManager:
        replicas: 2

  releaseService:
    spec:
      releaseControllerManager:
        replicas: 2

  buildService:
    spec:
      buildControllerManager:
        replicas: 2

  # Internal registry (optional for production)
  internalRegistry:
    enabled: false

  # Image controller (requires Quay token)
  imageController:
    enabled: true

  certManager:
    createClusterIssuer: true
```

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
kubectl -n image-controller create secret generic quay-token \
  --from-literal=token="your-quay-token"
```

### Step 5: Verify Deployment

```bash
# Check Konflux status
kubectl get konflux konflux -o yaml

# Check component pods
kubectl get pods -A | grep konflux

# Wait for Ready condition
kubectl wait --for=condition=Ready konflux/konflux --timeout=15m
```

## Configuration

### Konflux CR Fields

The Konflux CR supports configuration for all components:

#### UI Component

```yaml
ui:
  spec:
    ingress:
      enabled: true
      ingressClassName: "nginx"
      host: "konflux.example.com"
      annotations:
        cert-manager.io/cluster-issuer: "letsencrypt-prod"
      tlsSecretName: "konflux-ui-tls"
    proxy:
      replicas: 3
      nginx:
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
    dex:
      config:
        enablePasswordDB: false  # Disable password DB for production
        connectors:
          - type: "github"
            id: "github"
            name: "GitHub"
            config:
              clientID: "$GITHUB_CLIENT_ID"
              clientSecret: "$GITHUB_CLIENT_SECRET"
              redirectURI: "https://konflux.example.com/idp/callback"
```

#### Build Service

```yaml
buildService:
  spec:
    buildControllerManager:
      replicas: 2
      manager:
        resources:
          requests:
            cpu: 100m
            memory: 512Mi
```

#### Integration Service

```yaml
integrationService:
  spec:
    integrationControllerManager:
      replicas: 2
      manager:
        resources:
          requests:
            cpu: 100m
            memory: 512Mi
```

#### Internal Registry

```yaml
internalRegistry:
  enabled: true  # Enable for Kind/local dev
  # enabled: false  # Disable for production (use external registry)
```

#### Image Controller

```yaml
imageController:
  enabled: true  # Requires Quay token secret
```

### Resource Sizing

**Local Development (Kind):**
- Minimal replicas (1)
- Reduced resource requests (30m CPU, 128Mi memory)
- Total cluster memory: 8-16GB

**Production:**
- Multiple replicas (2-3) for HA
- Production resource requests (100m+ CPU, 256Mi+ memory)
- Horizontal scaling based on load

## Authentication

Konflux uses Dex for authentication. Production deployments should use OIDC connectors.

### GitHub Connector

```yaml
dex:
  config:
    connectors:
      - type: "github"
        id: "github"
        name: "GitHub"
        config:
          clientID: "$GITHUB_CLIENT_ID"
          clientSecret: "$GITHUB_CLIENT_SECRET"
          redirectURI: "https://konflux.example.com/idp/callback"
          orgs:
            - name: "your-org"
              teams:
                - "developers"
```

### Google OIDC Connector

```yaml
dex:
  config:
    connectors:
      - type: "oidc"
        id: "google"
        name: "Google"
        config:
          clientID: "$GOOGLE_CLIENT_ID"
          clientSecret: "$GOOGLE_CLIENT_SECRET"
          redirectURI: "https://konflux.example.com/idp/callback"
          issuer: "https://accounts.google.com"
```

### LDAP Connector

```yaml
dex:
  config:
    connectors:
      - type: "ldap"
        id: "ldap"
        name: "Corporate LDAP"
        config:
          host: "ldap.example.com:636"
          bindDN: "cn=admin,dc=example,dc=com"
          bindPW: "$LDAP_BIND_PASSWORD"
          userSearch:
            baseDN: "ou=Users,dc=example,dc=com"
            filter: "(objectClass=person)"
            username: "uid"
```

**Never use staticPasswords in production.** The operator samples do not include demo users for this reason.

## Demo Resources

Demo resources are for **testing only** and should never be deployed in production.

### When to Use Demo Resources

- Local Kind clusters for development
- Testing authentication flows
- Demo environments

### Security Warning

Demo users have hardcoded passwords (`password`) that are publicly known. They provide **no security** and must never be used in production environments.

### Deploying Demo Resources

On any cluster with Konflux installed:

```bash
./scripts/deploy-demo-resources.sh
```

This deploys:
- Demo users: `user1@konflux.dev`, `user2@konflux.dev` (password: `password`)
- Demo namespaces with RBAC
- Dex configuration with staticPasswords

### Removing Demo Resources

```bash
kubectl delete configmap dex -n dex
kubectl delete -k test/resources/demo-users/user/
# Restore original Dex configuration or re-apply your Konflux CR
```

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
# In scripts/deploy-local-dev.env
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

### ARM64 / Apple Silicon

The setup scripts automatically detect ARM64 and use `kind-config-arm64.yaml`. No manual configuration needed.

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

- [Mac Setup Guide](mac-setup.md) - macOS-specific setup instructions
- [Operator Samples](../operator/config/samples/README.md) - Example Konflux CRs
- [Konflux Documentation](https://konflux-ci.dev/docs/) - Full Konflux documentation
