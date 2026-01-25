# Konflux-CI

Konflux is a cloud-native CI/CD system for building, testing, and releasing applications on Kubernetes.

## Quick Start

Deploy Konflux locally using the operator-based installer. This works on any Kubernetes cluster, with convenience scripts for local Kind clusters.

### Prerequisites

Install these tools before deploying Konflux:

- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) (v0.26.0+) for local clusters
- kubectl (v1.31.4+)
- Podman (v5.3.1+) or Docker (v27.0.1+)
- git (v2.46+)
- openssl (v3.0.13+)

**macOS users:** See the [Mac Setup Guide](docs/mac-setup.md) for platform-specific configuration requirements including Podman machine setup and port conflict resolution.

### Local Development Setup

Deploy Konflux on a Kind cluster in three steps.

Clone this repository:

```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci
```

Create configuration files from templates:

```bash
cp my-konflux.yaml.template my-konflux.yaml
cp scripts/deploy-local-dev.env.template scripts/deploy-local-dev.env
```

Edit `scripts/deploy-local-dev.env` with your GitHub App credentials. See [GitHub Integration](#github-integration) for setup instructions, or refer to the template file which includes obtaining these values.

Deploy Konflux:

```bash
./scripts/deploy-local-dev.sh my-konflux.yaml
```

The deployment script creates a Kind cluster, installs the Konflux operator, applies your configuration, and sets up GitHub integration. Access the UI at https://localhost:9443 when deployment completes.

The cluster includes an internal OCI registry at localhost:5001 for storing container images during development.

### Production Deployment

For production deployments on OpenShift, EKS, GKE, or other Kubernetes clusters, see the [Operator Deployment Guide](docs/operator-deployment.md). Production deployments require manual operator installation and custom configuration for ingress, authentication, and secrets management.

## Demo Users (Testing Only)

**WARNING:** Demo users are for TESTING ONLY and must NEVER be used in production environments. They use publicly known passwords.

The automated setup script deploys demo users by default for local development. To disable this secure-by-default behavior, set `DEPLOY_DEMO_RESOURCES=0` in `scripts/deploy-local-dev.env`.

Demo credentials:
- Username: `user1@konflux.dev`
- Password: `password`

For complete demo user documentation including manual setup, custom users, and removal instructions, see [Demo Users Configuration](docs/demo-users.md).

Production deployments should configure proper identity providers through the Konflux CR. See the [Operator Deployment Guide](docs/operator-deployment.md#authentication) for examples using GitHub OAuth, Google OIDC, and LDAP connectors.

## GitHub Integration

Konflux triggers pipelines through GitHub webhooks. You need a GitHub App to enable this integration.

Create a GitHub App following the [Pipelines-as-Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup). This is the authoritative source for GitHub App setup and is maintained alongside the Pipelines-as-Code project.

For local Kind clusters, GitHub cannot reach services inside the cluster. Use [smee](https://smee.io/) as a webhook proxy. The deployment script creates a smee channel and deploys a client to relay events to Pipelines-as-Code.

Find your smee channel URL:

```bash
grep value dependencies/smee/smee-channel-id.yaml
```

Use this URL as the Webhook URL when creating your GitHub App. For the Homepage URL, use https://localhost:9443.

Generate and download the private key when creating the app. Create secrets in three namespaces to enable GitHub integration across Konflux components:

```bash
for ns in pipelines-as-code build-service integration-service; do
  kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
    --from-file=github-private-key=/path/to/github-app.pem \
    --from-literal=github-application-id="123456" \
    --from-literal=webhook.secret="your-webhook-secret"
done
```

For complete GitHub integration instructions including repository secrets and app installation, see [Configuring GitHub Application Secrets](./docs/github-secrets.md).

## Building and Running the Operator from Source

The operator manages the Konflux deployment lifecycle. For development work on the operator itself, see the [Getting Started](./operator/README.md#getting-started) section in the operator README.

## Namespace and User Management

**Prerequisite for using Konflux:** Create a namespace before onboarding applications.

Create a namespace and label it for Konflux use:

```bash
kubectl create namespace user-ns3
kubectl label namespace user-ns3 konflux-ci.dev/type=tenant
```

Grant yourself access to the namespace:

```bash
kubectl create rolebinding my-access \
  --clusterrole konflux-admin-user-actions \
  --user your-email@example.com \
  -n user-ns3
```

Replace `your-email@example.com` with the email address from your identity provider configuration.

**Note:** If you deployed demo resources, the namespaces `user-ns1` and `user-ns2` are already created with appropriate access for demo users.

For complete instructions including namespace labeling requirements and RBAC configuration, see [Namespace and User Management](docs/operator-deployment.md#namespace-and-user-management).

## Using Konflux

**Prerequisite:** You need a namespace with user access. See the previous section if you haven't created one.

Konflux builds, tests, and releases applications from Git repositories. You can onboard applications through the web UI or Kubernetes manifests, configure integration tests to verify builds, and release container images to registries.

For a complete tutorial including onboarding, integration tests, releases, and registry configuration, see [Using Konflux](docs/using-konflux.md).

For comprehensive documentation including advanced workflows and API references, see the [Konflux Documentation](https://konflux-ci.dev/docs/).

## Resource Management

The operator manages resource allocation for Konflux components through the Konflux CR specification. Configure resource requests and limits for components, and use Kyverno policies to manage pipeline workload resources.

For details on configuring component resources and managing pipeline workload resources, see [Resource Management](docs/operator-deployment.md#resource-management).

## Troubleshooting

Common issues and solutions:

- **Pipelines not triggering:** Verify GitHub App installation and smee channel configuration. See [troubleshooting guide](./docs/troubleshooting.md#pr-changes-are-not-triggering-pipelines).

- **Resource exhaustion:** Increase inotify limits, PID limits, or cluster memory allocation. See [resource troubleshooting](./docs/troubleshooting.md#running-out-of-resources).

- **UI not accessible:** Verify NodePort configuration for Kind clusters. See [operator deployment troubleshooting](./docs/troubleshooting.md#ui-not-accessible-kind).

- **macOS-specific issues:** Check Podman machine configuration and port conflicts. See [Mac Setup Guide](docs/mac-setup.md#common-issues).

For comprehensive troubleshooting including Docker rate limits, PVC binding issues, and release failures, see [Troubleshooting Common Issues](./docs/troubleshooting.md).

## Documentation

- [Operator Deployment Guide](docs/operator-deployment.md) - Production deployment instructions
- [Demo Users Configuration](docs/demo-users.md) - Testing authentication setup
- [Mac Setup Guide](docs/mac-setup.md) - macOS-specific configuration
- [Troubleshooting Guide](docs/troubleshooting.md) - Common issues and solutions
- [Configuring GitHub Secrets](./docs/github-secrets.md) - GitHub App integration
- [Registry Configuration](./docs/registry-configuration.md) - Container registry setup and configuration
- [Release Process](./RELEASE.md) - Project release procedures
- [Contributing Guidelines](./CONTRIBUTING.md) - How to contribute
