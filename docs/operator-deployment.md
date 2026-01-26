# Konflux Operator Deployment Guide

This guide covers deploying Konflux using the operator on any Kubernetes cluster, from local Kind clusters to production environments.

## Overview

The Konflux operator manages the complete lifecycle of Konflux installations. It deploys and configures all components declaratively through Custom Resources, replacing the legacy bootstrap scripts.

**Why use the operator:**

The operator provides declarative configuration through the Konflux CR, works on any Kubernetes cluster (not just Kind), supports high availability with multiple replicas, enables proper secret management, and simplifies upgrades through CR changes.

**Deployment approaches:**

For local development on Kind clusters, use automated scripts that handle cluster creation, operator deployment, and configuration. For production deployments on OpenShift, EKS, GKE, or other clusters, manually install the operator and configure components for your environment.

## Prerequisites

All deployments require kubectl (v1.31.4+), git (v2.46+), and access to a Kubernetes cluster (v1.28+).

Local development additionally needs Kind (v0.26.0+) and Podman (v5.3.1+) or Docker (v27.0.1+).

**macOS users:** Podman requires a virtual machine on macOS. See the [Mac Setup Guide](mac-setup.md) for Podman machine configuration, memory requirements, and port conflict resolution.

## Local Development Setup

Deploy Konflux on a Kind cluster using automated scripts.

### Configuration

Create configuration files from templates:

```bash
cp my-konflux.yaml.template my-konflux.yaml
cp scripts/deploy-local-dev.env.template scripts/deploy-local-dev.env
```

Edit `scripts/deploy-local-dev.env` with your secrets:

```bash
# GitHub App credentials (required)
GITHUB_PRIVATE_KEY_PATH="/path/to/github-app.pem"
GITHUB_APP_ID="123456"
WEBHOOK_SECRET="your-webhook-secret"

# Quay.io token (optional - only needed for image-controller)
QUAY_TOKEN=""

# Deployment options
DEPLOY_DEMO_RESOURCES=1  # Set to 0 to skip demo users
REGISTRY_HOST_PORT=5001  # Host port for internal registry
```

The template file includes detailed instructions for obtaining GitHub App credentials and generating webhook secrets.

### Deployment

Deploy Konflux with a single command:

```bash
./scripts/deploy-local-dev.sh my-konflux.yaml
```

The script performs these steps: creates a Kind cluster with appropriate resource allocation, deploys prerequisite operators (Tekton, cert-manager), installs the Konflux operator, applies your Konflux CR configuration, creates GitHub integration secrets in required namespaces, deploys demo users if enabled, and sets up smee webhook proxy for local development.

Access the UI at https://localhost:9443 when deployment completes.

The cluster includes an internal OCI registry accessible at localhost:5001 from your host and as `registry-service.kind-registry.svc.cluster.local` from within the cluster.

### What Gets Deployed

The local deployment includes all Konflux components: UI with ingress configuration, build-service for managing builds, integration-service for test orchestration, release-service for publishing releases, internal OCI registry, and Dex authentication with demo users (if enabled).

Supporting infrastructure: Tekton Pipelines for CI/CD execution, cert-manager for TLS certificate management, trust-manager for certificate distribution, Kyverno for resource policies, and smee webhook proxy for GitHub integration.

### Demo Users

**WARNING:** Demo users are for TESTING ONLY and must NEVER be used in production.

The deployment script enables demo users by default for local development convenience. These users have publicly known passwords and provide no security.

Demo credentials:
- Username: `user1@konflux.dev` or `user2@konflux.dev`
- Password: `password`

To disable demo users, set `DEPLOY_DEMO_RESOURCES=0` in `scripts/deploy-local-dev.env` before deployment.

For complete demo user documentation including manual configuration and removal, see [Demo Users Configuration](demo-users.md).

## Production Deployment

Deploy Konflux on production Kubernetes clusters with manual configuration.

### Step 1: Clone Repository

```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci/operator
```

### Step 2: Install Prerequisites

Production clusters need Tekton Pipelines, cert-manager, and trust-manager installed before deploying the operator.

Install Tekton Pipelines:

```bash
kubectl apply -f https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml
```

Install cert-manager:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.2/cert-manager.yaml
```

Install trust-manager:

```bash
helm repo add jetstack https://charts.jetstack.io
helm install trust-manager jetstack/trust-manager \
  --namespace cert-manager \
  --wait
```

Verify installations complete before proceeding:

```bash
kubectl wait --for=condition=Available deployment/tekton-pipelines-controller -n tekton-pipelines --timeout=300s
kubectl wait --for=condition=Available deployment/cert-manager -n cert-manager --timeout=300s
kubectl wait --for=condition=Available deployment/trust-manager -n cert-manager --timeout=300s
```

### Step 3: Deploy Operator

Install the Konflux CRDs:

```bash
make install
```

Deploy the operator. For production, use the released operator image:

```bash
make deploy IMG=quay.io/konflux-ci/konflux-operator:latest
```

For development or custom builds, build and push your own image:

```bash
make docker-build docker-push IMG=<your-registry>/konflux-operator:tag
make deploy IMG=<your-registry>/konflux-operator:tag
```

Wait for the operator to become ready:

```bash
kubectl wait --for=condition=Available deployment/konflux-operator-controller-manager -n konflux-operator --timeout=300s
```

### Step 4: Create Secrets

Konflux requires GitHub integration secrets in three namespaces.

Create the pipelines-as-code namespace if it doesn't exist:

```bash
kubectl create namespace pipelines-as-code
```

Create GitHub App secrets:

```bash
for ns in pipelines-as-code build-service integration-service; do
  kubectl create namespace ${ns} --dry-run=client -o yaml | kubectl apply -f -
  kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
    --from-file=github-private-key=/path/to/github-app.pem \
    --from-literal=github-application-id="123456" \
    --from-literal=webhook.secret="your-webhook-secret"
done
```

For image-controller (if you enable it in the Konflux CR):

```bash
kubectl create namespace image-controller
kubectl -n image-controller create secret generic quay-token \
  --from-literal=token="your-quay-token"
```

See [Configuring GitHub Application Secrets](github-secrets.md) for detailed GitHub App setup instructions.

### Step 5: Configure Konflux CR

Create a `konflux.yaml` file with your configuration. Start with the production template:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  # UI configuration with ingress
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
          # Production: Use real identity providers, not static passwords
          enablePasswordDB: false
          connectors:
            - type: "github"
              id: "github"
              name: "GitHub"
              config:
                clientID: "$GITHUB_CLIENT_ID"
                clientSecret: "$GITHUB_CLIENT_SECRET"
                redirectURI: "https://konflux.example.com/idp/callback"
                orgs:
                  - name: "your-organization"

  # Build service
  buildService:
    spec:
      buildControllerManager:
        replicas: 2
        manager:
          resources:
            requests:
              cpu: 100m
              memory: 512Mi

  # Integration service
  integrationService:
    spec:
      integrationControllerManager:
        replicas: 2
        manager:
          resources:
            requests:
              cpu: 100m
              memory: 512Mi

  # Release service
  releaseService:
    spec:
      releaseControllerManager:
        replicas: 2
        manager:
          resources:
            requests:
              cpu: 100m
              memory: 512Mi

  # Internal registry (disable for production - use external registry)
  internalRegistry:
    enabled: false

  # Image controller (requires Quay token)
  imageController:
    enabled: true

  # Certificate management
  certManager:
    createClusterIssuer: true
```

### Step 6: Deploy Konflux

Apply your configuration:

```bash
kubectl apply -f konflux.yaml
```

Monitor the deployment:

```bash
# Watch Konflux status
kubectl get konflux konflux -o jsonpath='{.status.conditions}' | jq

# Watch component pods
kubectl get pods -A | grep konflux

# Wait for Ready condition
kubectl wait --for=condition=Ready konflux/konflux --timeout=15m
```

The operator creates component namespaces and deploys services based on your CR specification. Check the status to identify any configuration issues.

### Step 7: Verify Deployment

Verify all components are running:

```bash
# Check component namespaces
kubectl get namespaces | grep konflux

# Check pod status
kubectl get pods -n konflux-ui
kubectl get pods -n build-service
kubectl get pods -n integration-service
kubectl get pods -n release-service

# Verify ingress
kubectl get ingress -n konflux-ui
```

Test UI access at your configured host (e.g., https://konflux.example.com).

## Configuration Reference

### UI Component

The UI component provides the web interface and handles authentication.

**Basic ingress configuration:**

```yaml
ui:
  spec:
    ingress:
      enabled: true
      ingressClassName: "nginx"  # Or "openshift-default" on OpenShift
      host: "konflux.example.com"
      tlsSecretName: "konflux-ui-tls"
```

**Cert-manager integration:**

```yaml
ui:
  spec:
    ingress:
      annotations:
        cert-manager.io/cluster-issuer: "letsencrypt-prod"
```

**Kind cluster NodePort (local development only):**

```yaml
ui:
  spec:
    ingress:
      nodePortService:
        httpsPort: 30011  # Maps to host port 9443 via Kind config
```

Kind clusters cannot use standard Ingress resources from the host. The NodePort configuration creates a service accessible from the host. The Kind config file maps the NodePort to localhost:9443.

**Replica configuration:**

```yaml
ui:
  spec:
    proxy:
      replicas: 3  # HA deployment
    dex:
      replicas: 2  # HA deployment
```

**Resource allocation:**

```yaml
ui:
  spec:
    proxy:
      nginx:
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 200m
            memory: 512Mi
```

### Authentication

Production deployments should use proper identity providers through Dex connectors.

**GitHub OAuth:**

```yaml
dex:
  config:
    enablePasswordDB: false
    connectors:
      - type: "github"
        id: "github"
        name: "GitHub"
        config:
          clientID: "$GITHUB_CLIENT_ID"
          clientSecret: "$GITHUB_CLIENT_SECRET"
          redirectURI: "https://konflux.example.com/idp/callback"
          orgs:
            - name: "your-organization"
              teams:
                - "developers"
```

**Google OIDC:**

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

**LDAP:**

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

Never use `staticPasswords` in production. The operator samples exclude demo users by design.

### Service Components

**Build Service:**

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

**Integration Service:**

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

**Release Service:**

```yaml
releaseService:
  spec:
    releaseControllerManager:
      replicas: 2
      manager:
        resources:
          requests:
            cpu: 100m
            memory: 512Mi
```

### Optional Components

**Internal Registry:**

```yaml
internalRegistry:
  enabled: true  # Enable for local development
  # enabled: false  # Disable for production (use external registry)
```

The internal registry provides a simple development registry but lacks production features like replication, backup, and access controls. Production deployments should use external registries like Quay.io, Docker Hub, or cloud provider registries.

**Image Controller:**

```yaml
imageController:
  enabled: true  # Requires Quay token in image-controller namespace
```

The image controller automatically provisions Quay.io repositories for components. This requires a Quay.io application token stored in a secret. See [automatic repository provisioning documentation](registry-configuration.md#automatic-repository-provisioning-quayio) for configuration details.

## Resource Sizing

Resource requirements scale with deployment size and load.

### Local Development

For Kind clusters with minimal load:

```yaml
buildService:
  spec:
    buildControllerManager:
      manager:
        resources:
          requests:
            cpu: 30m
            memory: 128Mi
```

Typical local cluster allocation: 4 CPU cores, 8-12 GB RAM total. Set Kind memory allocation in `scripts/deploy-local-dev.env` with `KIND_MEMORY_GB=12`.

**macOS users:** Ensure Podman machine has sufficient memory (KIND_MEMORY_GB + 4GB overhead). See [Mac Setup Guide](mac-setup.md#podman-machine-configuration) for Podman resource configuration.

### Production

For production with high availability:

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
          limits:
            cpu: 500m
            memory: 1Gi
```

Production clusters typically allocate: 8+ CPU cores, 32+ GB RAM, multiple replicas for each service, resource limits to prevent resource exhaustion, and horizontal pod autoscaling for variable load.

Monitor resource usage and adjust allocations:

```bash
kubectl top nodes
kubectl top pods -A | grep konflux
```

## Manual Local Deployment

For users who want fine-grained control over local deployments, manually deploy each component instead of using the automated script.

### Create Kind Cluster

Create a cluster with the appropriate configuration:

```bash
# Detect architecture and use correct config
if [ "$(uname -m)" = "arm64" ]; then
  kind create cluster --name konflux --config kind-config-arm64.yaml
else
  kind create cluster --name konflux --config kind-config.yaml
fi
```

For Podman environments, set the provider:

```bash
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster --name konflux --config kind-config-arm64.yaml
```

Increase system limits to avoid resource exhaustion:

```bash
# Increase inotify limits
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512

# Increase PID limit on Kind container (Podman)
podman update --pids-limit 4096 konflux-control-plane

# Or for Docker
docker update --pids-limit 4096 konflux-control-plane
```

### Deploy Dependencies

Deploy prerequisites while skipping components managed by the operator:

```bash
SKIP_DEX=true \
SKIP_KONFLUX_INFO=true \
SKIP_CLUSTER_ISSUER=true \
SKIP_INTERNAL_REGISTRY=true \
./deploy-deps.sh
```

This deploys Tekton Pipelines, cert-manager, trust-manager, Kyverno, and smee webhook proxy.

### Install Operator

Install from the latest release:

```bash
kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/latest/download/install.yaml
```

Wait for the operator:

```bash
kubectl wait --for=condition=Available deployment/konflux-operator-controller-manager -n konflux-operator --timeout=300s
```

### Deploy Konflux CR

Use the sample configuration:

```bash
kubectl apply -f <(curl -L \
  https://github.com/konflux-ci/konflux-ci/releases/latest/download/samples.tar.gz | \
  tar -xzO ./konflux_v1alpha1_konflux.yaml)
```

Or create your own `my-konflux.yaml` based on the template and apply it:

```bash
kubectl apply -f my-konflux.yaml
```

Wait for Konflux to be ready:

```bash
kubectl wait --for=condition=Ready=True konflux konflux --timeout=10m
```

### Deploy Demo Resources (Optional)

**WARNING:** Demo users are for TESTING ONLY.

```bash
./scripts/deploy-demo-resources.sh
```

This creates demo users, namespaces, and RBAC. See [Demo Users Configuration](demo-users.md) for details.

## Upgrading Konflux

Upgrade Konflux by updating the operator and Konflux CR.

### Upgrade Operator

For operators installed from releases:

```bash
kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/download/v0.1.0/install.yaml
```

For operators deployed with make:

```bash
cd operator
git pull
make deploy IMG=quay.io/konflux-ci/konflux-operator:v0.1.0
```

### Update Configuration

Edit your Konflux CR to change component versions, resource allocations, or enabled features:

```bash
kubectl edit konflux konflux
```

The operator reconciles changes and updates components. Monitor the rollout:

```bash
kubectl get konflux konflux -o jsonpath='{.status.conditions}' | jq
```

## Resource Management

Konflux components and user workloads consume CPU and memory. The operator manages resource allocation for Konflux components through the Konflux CR specification.

### Konflux Components

Configure resource requests and limits in your Konflux CR:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  buildService:
    spec:
      buildControllerManager:
        manager:
          resources:
            requests:
              cpu: 30m
              memory: 128Mi
            limits:
              cpu: 30m
              memory: 128Mi
```

See the [sample Konflux CR](../operator/config/samples/konflux_v1alpha1_konflux.yaml) for examples across all components. The [Konflux Operator API Reference](https://konflux-ci.dev/konflux-ci/docs/reference/) provides complete field documentation.

### Pipeline Configuration Management

The build-service manages pipeline configurations through a ConfigMap named `build-pipeline-config` in the build-service namespace. By default, the operator creates and maintains this ConfigMap with references to the standard Konflux pipeline bundles (docker-build-oci-ta, fbc-builder, etc.).

For advanced use cases requiring custom pipeline bundles, you can disable operator management and provide your own ConfigMap.

**Use cases for custom pipeline management:**

Users implementing custom workflows, organizations maintaining proprietary pipeline bundles, or environments using external configuration management tools (Helm, ArgoCD) may need direct control over pipeline definitions.

**Transition to self-managed pipeline configuration:**

Update your Konflux CR to disable operator management:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  buildService:
    spec:
      managePipelineConfig: false
      buildControllerManager:
        # ... your other configuration
```

Apply the change:

```bash
kubectl apply -f my-konflux.yaml
```

Delete the operator-managed ConfigMap:

```bash
kubectl delete configmap build-pipeline-config -n build-service
```

Create your custom pipeline configuration:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: build-pipeline-config
  namespace: build-service
data:
  config.yaml: |
    default-pipeline-name: my-custom-pipeline
    pipelines:
    - name: my-custom-pipeline
      bundle: quay.io/my-org/my-pipeline@sha256:abc123...
    - name: docker-build-oci-ta
      bundle: quay.io/konflux-ci/tekton-catalog/pipeline-docker-build-oci-ta@sha256:...
```

Apply your custom configuration:

```bash
kubectl apply -f my-pipeline-config.yaml
```

**Important notes:**

The ConfigMap must be named `build-pipeline-config` and exist in the `build-service` namespace. The operator will not create or update the ConfigMap when `managePipelineConfig: false` is set. You are responsible for maintaining and updating pipeline bundle references. To return to operator-managed configuration, set `managePipelineConfig: true` (or remove the field) and delete your custom ConfigMap.

See the [Konflux sample CR](../operator/config/samples/konflux_v1alpha1_konflux.yaml) for a complete configuration example. The [KonfluxBuildService sample CR](../operator/config/samples/konflux_v1alpha1_konfluxbuildservice.yaml) shows additional details about the build service configuration and pipeline management.

### Pipeline Workloads

Tekton TaskRuns create pods that may not specify resource requirements. Use [Kyverno](https://kyverno.io/) policies to mutate pod resource requests at creation time.

The deployment includes policies that reduce resource requirements for specific tasks. See [this example](../dependencies/kyverno/policy/pod-requests-del.yaml) which reduces resources for Enterprise Contract verification tasks.

Add policies for other resource-intensive tasks by matching pod labels. Tekton's [automatic labeling](https://tekton.dev/docs/pipelines/labels/#automatic-labeling) propagates task and pipeline names to pods.

## Namespace and User Management

### Creating Namespaces

Create a namespace and label it for Konflux use:

```bash
kubectl create namespace user-ns3
kubectl label namespace user-ns3 konflux-ci.dev/type=tenant
```

The `konflux-ci.dev/type=tenant` label identifies the namespace as a Konflux tenant namespace and enables proper RBAC policies.

### Granting User Access

Create a RoleBinding to grant a user access to a namespace:

```bash
kubectl create rolebinding user1-konflux \
  --clusterrole konflux-admin-user-actions \
  --user user1@konflux.dev \
  -n user-ns3
```

Replace `user1@konflux.dev` with the email address from your identity provider configuration.

The `konflux-admin-user-actions` ClusterRole grants permissions to create and manage Applications, Components, IntegrationTestScenarios, and other Konflux resources.

### Managing Users

For production deployments, configure identity providers through the Konflux CR. See the [Authentication](#authentication) section for examples using GitHub OAuth, Google OIDC, and LDAP connectors.

For demo users (local testing only), see the [Demo Users Guide](demo-users.md).

## Troubleshooting

### Operator Not Starting

Check operator logs:

```bash
kubectl logs -n konflux-operator deployment/konflux-operator-controller-manager
```

Verify CRDs installed:

```bash
kubectl get crds | grep konflux
```

Missing CRDs indicate incomplete operator installation. Run `make install` from the operator directory.

### Components Not Deploying

Check Konflux status:

```bash
kubectl get konflux konflux -o jsonpath='{.status.conditions}' | jq
```

Check operator events:

```bash
kubectl get events -n konflux-operator --sort-by='.lastTimestamp'
```

Common issues include missing secrets (verify GitHub App secrets exist), insufficient cluster resources (check node allocations), invalid CR configuration (verify against sample), or prerequisite operators not running (ensure Tekton, cert-manager installed).

### UI Not Accessible (Kind)

For Kind clusters, verify NodePort configuration in your Konflux CR:

```yaml
ui:
  spec:
    ingress:
      nodePortService:
        httpsPort: 30011
```

If missing, patch the KonfluxUI CR:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  ingress:
    nodePortService:
      httpsPort: 30011
'
```

Verify the Kind config includes the port mapping in `kind-config-arm64.yaml`:

```yaml
extraPortMappings:
  - containerPort: 30011
    hostPort: 9443
    protocol: TCP
```

Browser must use `https://localhost:9443` not `http://`. Typing `localhost:9443` defaults to HTTP and fails.

### Port Conflicts (macOS)

Port 5000 is often used by macOS AirPlay Receiver.

Disable AirPlay Receiver: System Settings → General → AirDrop & Handoff → AirPlay Receiver → Off.

Or use a different port in `scripts/deploy-local-dev.env`:

```bash
REGISTRY_HOST_PORT=5001
```

See [Mac Setup Guide](mac-setup.md#port-5000-conflict-resolution) for additional port conflict solutions.

### Insufficient Memory (Kind)

Check Podman machine memory allocation:

```bash
podman machine inspect | grep Memory
```

Increase memory by recreating the machine:

```bash
podman machine stop
podman machine rm konflux-dev
podman machine init --memory 16384 --cpus 6 --rootful konflux-dev
podman machine start konflux-dev
```

See [Mac Setup Guide](mac-setup.md#memory-and-cpu-recommendations) for detailed resource sizing guidance.

### Dex Not Starting

Check Dex logs:

```bash
kubectl logs -n konflux-ui deployment/dex
```

Verify Dex configuration in the KonfluxUI CR:

```bash
kubectl get konfluxui konflux-ui -n konflux-ui -o jsonpath='{.spec.dex.config}' | jq
```

Common issues include invalid connector configuration, missing client secrets, or conflicting connectors (ensure enablePasswordDB is false when using connectors).

### Secrets Not Found

Verify secrets exist in required namespaces:

```bash
kubectl get secrets -n pipelines-as-code | grep pipelines-as-code-secret
kubectl get secrets -n build-service | grep pipelines-as-code-secret
kubectl get secrets -n integration-service | grep pipelines-as-code-secret
```

Recreate missing secrets following Step 4 in production deployment.

## Related Documentation

- [Mac Setup Guide](mac-setup.md) - macOS-specific configuration
- [Demo Users Configuration](demo-users.md) - Testing authentication setup
- [Troubleshooting Guide](troubleshooting.md) - Common issues and solutions
- [Konflux Operator API Reference](https://konflux-ci.dev/konflux-ci/docs/reference/) - Complete API documentation
- [Operator Samples](../operator/config/samples/) - Example Konflux CRs
