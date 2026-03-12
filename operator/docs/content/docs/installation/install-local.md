---
title: "Local Deployment (Kind)"
linkTitle: "Local Deployment (Kind)"
weight: 1
description: "Deploying Konflux locally on macOS or Linux using Kind."
---

This guide walks you through deploying Konflux locally on macOS or Linux using
[Kind](https://kind.sigs.k8s.io/). The automated `deploy-local.sh` script handles
cluster creation, operator deployment, and GitHub integration in a single step.

## Prerequisites

Verify that the following tools are installed:

| Tool | Minimum version |
|------|----------------|
| [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) | v0.26.0 |
| [podman](https://podman.io/docs/installation) or [docker](https://docs.docker.com/engine/install/) | podman v5.3.1 / docker v27.0.1 |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | v1.31.4 |
| [git](https://git-scm.com/) | v2.46 |
| [openssl](https://www.openssl.org/) | v3.0.13 |


- A **GitHub Application** with a private key - Konflux uses it to receive webhook events
  from GitHub, trigger build pipelines on pull requests, and write pipeline status back
  to the PR. For local Kind clusters not reachable from the internet, a smee proxy is
  used to relay webhook events into the cluster. See
  [GitHub Application Secrets]({{< relref "github-secrets" >}}) for instructions on
  creating one.

**Minimum host resources (free):**
- CPU: 4 cores
- RAM: 8 GB

{{< alert color="info" >}}
If you have both Docker and Podman installed and prefer to use Podman, set the provider
explicitly before running the script:
<pre><code>export KIND_EXPERIMENTAL_PROVIDER=podman</code></pre>
{{< /alert >}}

## Setup

1. Clone the repository:

```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci
```

2. Create the local configuration file from the template and fill in your GitHub App
   credentials:

```bash
cp scripts/deploy-local.env.template scripts/deploy-local.env
# Edit scripts/deploy-local.env with your values
```

See [Configuration options](#configuration-options) for a full reference of all available variables.
macOS-specific configuration is handled automatically by the script.

3. Deploy Konflux:

```bash
./scripts/deploy-local.sh
```

The script performs all of the following automatically:

- Creates a Kind cluster with proper resource allocation
- Increases inotify and PID limits
- Deploys the Konflux operator (using the method set by `OPERATOR_INSTALL_METHOD`)
- Applies the Konflux CR configuration
- Sets up GitHub App integration and smee webhook proxy
- Provides a local OCI registry at `localhost:5001`

## Verify the installation

Once the script completes, open **https://localhost:9443** in your browser and log in
with the demo credentials:

- **Username:** `user1@konflux.dev`
- **Password:** `password`

{{< alert color="info" >}}
<strong>Remote machine?</strong> If Kind is running on a remote host, open an SSH tunnel
to access the UI from your local browser:
<pre><code>ssh -L 9443:localhost:9443 $USER@$VM_IP</code></pre>
{{< /alert >}}

{{< alert color="warning" >}}
The demo users use static passwords and are intended for <strong>local development only</strong>.
Never use this configuration in a production environment. For production, configure
an OIDC connector instead.
{{< /alert >}}

## What gets deployed

The script always sets up the base infrastructure, regardless of the install method chosen.
The Konflux operator and its managed components are only installed when `OPERATOR_INSTALL_METHOD`
is not `none`.

### All methods

| Component | Details |
|-----------|---------|
| Kind cluster | Single-node cluster with ingress on port 9443 |
| cert-manager | TLS certificate lifecycle management |
| trust-manager | CA bundle distribution across namespaces |
| Tekton + Pipelines as Code | Pipeline execution engine and GitHub-triggered pipeline automation |
| Kyverno | Policy engine for namespace and RBAC automation |
| smee client | Webhook proxy relay for GitHub events |

### `release`, `local` and `build` methods

| Component | Details |
|-----------|---------|
| Konflux Operator | Deploys and manages all Konflux components lifecycles |


When using `none`, the script stops after setting up the base infrastructure and secrets.
You then install and run the operator manually - see the `none` method workflow under
[Install method values](#install-method-values) below, and
[Building and Installing from Source]({{< relref "install-from-source" >}}).

## Configuration options

All options are set in `scripts/deploy-local.env` (copied from
`scripts/deploy-local.env.template`). They can also be passed as environment variables
directly:

```bash
OPERATOR_INSTALL_METHOD=build ./scripts/deploy-local.sh
```

### Operator configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OPERATOR_INSTALL_METHOD` | `release` | How the operator is installed. See [install method values](#install-method-values) below |
| `OPERATOR_IMAGE` | `quay.io/konflux-ci/konflux-operator:latest` | Operator image used with the `local` and `build` methods |
| `KONFLUX_CR` | *(auto-selected)* | Path to the Konflux CR file to apply. Available samples are in `operator/config/samples/`. Can also be passed as a positional argument to the script |

#### Install method values

| Value | Description | When to use |
|-------|-------------|-------------|
| `release` *(default)* | Installs from the latest GitHub release (`install.yaml`) | Normal local development |
| `local` | Deploys from your current checkout using kustomize, with the latest released image | Testing manifest changes against a specific release image |
| `build` | Builds the operator image locally before deploying | Operator development - testing code changes |
| `none` | Sets up Kind + dependencies + secrets, then exits without installing the operator | Running the operator manually - see [Building and Installing from Source]({{< relref "install-from-source" >}}) |

{{< alert color="info" >}}
When using <code>local</code>, the manifests from your checkout are applied with the latest
released image. To avoid version mismatches, check out the matching release tag first:
<pre><code>git checkout v1.0.0
OPERATOR_INSTALL_METHOD=local ./scripts/deploy-local.sh</code></pre>
{{< /alert >}}

The default CR does **not** enable image-controller. If you set `QUAY_TOKEN` and
`QUAY_ORGANIZATION`, you must also use a CR that enables it (e.g. `konflux-e2e.yaml`):

```bash
KONFLUX_CR=operator/config/samples/konflux-e2e.yaml ./scripts/deploy-local.sh
```

If `QUAY_TOKEN` and `QUAY_ORGANIZATION` are both set and no CR is specified, the script
automatically selects `konflux-e2e.yaml`.

### Infrastructure configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `KIND_MEMORY_GB` | `8` | Memory allocated to the Kind cluster (GB). Minimum: 8, recommended: 16 for full stack |
| `REGISTRY_HOST_PORT` | `5001` | Host port for the internal OCI registry. Port 5000 is often taken by macOS AirPlay Receiver |
| `ENABLE_REGISTRY_PORT` | `1` | Expose the registry on the host (`0` to restrict to in-cluster access only) |
| `INCREASE_PODMAN_PIDS_LIMIT` | `1` | Increase Podman PID limits for Tekton pipeline performance (`0` to disable) |
| `PODMAN_MACHINE_NAME` | *(default machine)* | macOS only - name of the Podman machine to use when multiple machines exist |

### Secrets

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_APP_ID` | Yes | Numeric ID of your GitHub App (found in the App settings page) |
| `GITHUB_PRIVATE_KEY` | Yes¹ | Literal PEM private key content (multi-line, quoted) |
| `GITHUB_PRIVATE_KEY_PATH` | Yes¹ | Path to `.pem` file - takes precedence over `GITHUB_PRIVATE_KEY` |
| `WEBHOOK_SECRET` | Yes | Webhook secret for GitHub webhooks. Generate with: `openssl rand -hex 20` |
| `QUAY_TOKEN` | No² | Quay OAuth token for image-controller auto-provisioning. Generate at: Quay.io → Account Settings → Applications → Generate Token |
| `QUAY_ORGANIZATION` | No² | Quay organization where component images will be stored |
| `SMEE_CHANNEL` | No | Full Smee channel URL (e.g. `https://smee.io/XXXXXXXXXXXXXXXX`). Auto-generated if not set |

¹ Provide either `GITHUB_PRIVATE_KEY` or `GITHUB_PRIVATE_KEY_PATH`.

² `QUAY_TOKEN` and `QUAY_ORGANIZATION` have no effect unless you also set `KONFLUX_CR` to
a sample that enables image-controller (e.g. `operator/config/samples/konflux-e2e.yaml`).

The GitHub App must have the following permissions: `checks:write`, `contents:write`,
`issues:write`, `pull_requests:write`.

## What's next

- [Onboard a new Application]({{< relref "onboard" >}}) - onboard an application, run builds, tests, and releases
- [GitHub Application Secrets]({{< relref "github-secrets" >}}) - complete your GitHub App configuration
- [Registry Configuration]({{< relref "registry-configuration" >}}) - configure an external container registry for build and release pipelines
- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) - full CR field reference
- [Troubleshooting]({{< relref "troubleshooting" >}}) - solutions to common installation issues
- [Examples]({{< relref "../examples" >}}) - sample Konflux CR configurations
