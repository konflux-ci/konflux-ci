---
title: "Installing on OpenShift"
linkTitle: "Installing on OpenShift"
weight: 2
description: "Deploying Konflux on an existing OpenShift cluster using the automated deployment script."
---

This guide covers deploying Konflux on an existing OpenShift cluster using the
`deploy-konflux-on-ocp.sh` script. The script uses OpenShift-native components
(OpenShift Pipelines, Red Hat cert-manager) instead of their upstream alternatives.

{{< alert color="info" >}}
This is not the only way to install Konflux on OpenShift. You can also use:
<ul>
  <li><a href="{{< relref "install-olm" >}}">Installing from OLM</a> — install through the OpenShift OperatorHub</li>
  <li><a href="{{< relref "install-release" >}}">Installing from Release</a> — apply the pre-built release bundle directly</li>
  <li><a href="{{< relref "install-from-source" >}}">Building and Installing from Source</a> — build and run the operator from your local checkout</li>
</ul>
{{< /alert >}}

## Prerequisites

| Tool | Minimum version |
|------|----------------|
| OpenShift | v4.20 |
| `oc` or [kubectl](https://kubernetes.io/docs/tasks/tools/) | v1.31.4 |
| [git](https://git-scm.com/) | v2.46 |
| [make](https://www.gnu.org/software/make/) | — |
| [Go](https://go.dev/) | v1.26.0 |
| [openssl](https://www.openssl.org/) | v3.0.13 |

- `cluster-admin` permissions on the target OpenShift cluster.
- **GitHub App (Required for Build Pipelines)**: Konflux uses a GitHub App with [Pipelines-as-Code](https://pipelinesascode.com/) to receive webhook events, trigger build pipelines on pull requests, and report status back to GitHub.
  - Follow the [GitHub Application Secrets]({{< relref "github-secrets" >}}) guide to create your GitHub App.
  - While the Konflux Operator and UI can deploy without it, **build pipelines, pull request checks, and automated releases will not function until the GitHub App secrets are configured**.
  - You can export the GitHub App credentials before running the deployment script to deploy them automatically, or configure them right after deployment.

### Understanding Smee Webhook Proxy

[Smee](https://smee.io/) is an open-source webhook payload delivery service that proxies webhook payloads from GitHub over an outbound HTTP connection to a client running inside your cluster.

- **When Smee is NOT needed**: If your OpenShift cluster has a publicly accessible router/ingress, GitHub can deliver webhook events directly to your cluster's ingress URL. Smee is not needed in this case.
- **When Smee IS needed**: If your cluster is private, running behind a corporate firewall, or lacks a public ingress, GitHub cannot connect directly to it. In that scenario, specify a Smee channel URL via `SMEE_CHANNEL="https://smee.io/<channel-id>"` so Smee can relay webhook payloads into your cluster.

## Setup

1. Clone the repository:

```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci
```

2. Run the deployment script:

Choose one of the two options depending on whether you have created your GitHub App beforehand:

- **Option A (Recommended): Deploy with GitHub App credentials**  
  Export your GitHub App credentials so the script automatically deploys the integration secrets in Step 6:

  ```bash
  export GITHUB_APP_ID="<your-github-app-id>"
  export WEBHOOK_SECRET="<your-webhook-secret>"
  export GITHUB_PRIVATE_KEY_PATH="/path/to/your-private-key.pem"
  # Optional: If your cluster lacks public ingress, provide your Smee channel URL:
  # export SMEE_CHANNEL="https://smee.io/<channel-id>"

  ./deploy-konflux-on-ocp.sh
  ```

- **Option B: Deploy first, configure secrets later**  
  Run the script directly. The operator and Konflux CR will install, but build pipelines will remain unconfigured until you set up the secrets (see [Configure GitHub integration secrets](#configure-github-integration-secrets)):

  ```bash
  ./deploy-konflux-on-ocp.sh
  ```

To customise the operator image or Konflux CR before running, see [Script configuration](#script-configuration).

The script performs all of the following automatically:

- Deploys Konflux dependencies using OpenShift-native operators
- Installs the Konflux CRDs
- Deploys the Konflux Operator into the `konflux-operator` namespace
- Waits for the Operator to be ready
- Applies the OpenShift Konflux CR and waits for all components to reach `Ready`
- Deploys GitHub integration secrets if `GITHUB_APP_ID` is set

## What gets deployed

### Dependencies

| Component | Details |
|-----------|---------|
| OpenShift Pipelines | Installed via OLM (Red Hat's productized Tekton) |
| cert-manager | Installed via the Red Hat cert-manager OLM operator |
| MutatingAdmissionPolicies | CI/local: reduce Tekton TaskRun pod requests when the API is available (Kubernetes 1.36+) |
| Pipelines-as-Code | GitHub-triggered pipeline automation |
| Tekton Chains RBAC | RBAC for supply-chain signing using OpenShift namespaces |

The following components are not deployed by `deploy-deps.sh` in this configuration:

| Skipped | Reason |
|---------|--------|
| trust-manager | Not needed — OpenShift has native CA bundle injection via `config.openshift.io/inject-trusted-cabundle` |
| Internal OCI registry | OpenShift has its own integrated registry |
| Smee webhook proxy | Skipped by default (`SKIP_SMEE=true`). Not needed when the cluster has a public ingress URL. |

{{< alert color="warning" >}}
The script skips the Smee webhook proxy unless <code>SMEE_CHANNEL</code> is provided. For GitHub
to deliver webhook events (triggering build pipelines on pull requests), your cluster
must be reachable from the internet, or you must configure Smee as described in
<a href="#understanding-smee-webhook-proxy">Understanding Smee Webhook Proxy</a> and
<a href="{{< relref "github-secrets" >}}">GitHub Application Secrets</a>.
{{< /alert >}}

### Operator and Konflux

| Component | Details |
|-----------|---------|
| Konflux CRDs | `Konflux` custom resource definition |
| Konflux Operator | Deployed in the `konflux-operator` namespace |
| Konflux instance | All Konflux components managed by `konflux-openshift.yaml` |

## Verify the Konflux CR is ready

See [Applying the Konflux Custom Resource]({{< relref "apply-konflux-cr" >}}) for instructions
on verifying the Konflux CR status and accessing the UI URL.

```bash
kubectl wait --for=condition=Ready=True konflux konflux --timeout=15m
```

## Configure GitHub integration secrets

{{< alert color="warning" >}}
**Required for builds**: While the Konflux web console will be accessible, build pipelines
and pull request automation will **not** trigger until the GitHub App secrets are deployed.
{{< /alert >}}

If you did not export GitHub credentials before running `deploy-konflux-on-ocp.sh` (Option B),
configure the integration secrets now:

1. Create a GitHub App by following the [GitHub Application Secrets]({{< relref "github-secrets" >}}) guide.
2. Deploy the secrets to the required namespaces (`openshift-pipelines`, `build-service`, `integration-service`):

```bash
export GITHUB_APP_ID="<your-github-app-id>"
export WEBHOOK_SECRET="<your-webhook-secret>"
export GITHUB_PRIVATE_KEY_PATH="/path/to/your-private-key.pem"

USE_OPENSHIFT_PIPELINES=true ./scripts/deploy-secrets.sh
```

Alternatively, create the secrets manually:

```bash
for ns in openshift-pipelines build-service integration-service; do
  kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
    --from-file=github-private-key="/path/to/your-private-key.pem" \
    --from-literal=github-application-id="<your-github-app-id>" \
    --from-literal=webhook.secret="<your-webhook-secret>" \
    --dry-run=client -o yaml | kubectl apply -f -
done
```

## Script configuration

### Operator image

By default, the script uses `quay.io/konflux-ci/konflux-operator:latest`. To use a
different image, set `OPERATOR_IMAGE` before running:

```bash
OPERATOR_IMAGE=<your-registry>/konflux-operator:<tag> ./deploy-konflux-on-ocp.sh
```

To build and use a custom operator image from source:

```bash
cd operator
make docker-build docker-push IMG=<your-registry>/konflux-operator:<tag>
cd ..
OPERATOR_IMAGE=<your-registry>/konflux-operator:<tag> ./deploy-konflux-on-ocp.sh
```

### Konflux Custom Resource

The script applies `operator/config/samples/konflux-openshift.yaml` by default. That CR
contains no demo users and disables the Dex password database. Users sign in with their
existing cluster credentials through the OpenShift OAuth server, which the operator
configures automatically on OpenShift.

In OpenShift CI (Prow), `OPENSHIFT_CI=true` selects
`operator/config/samples/konflux-openshift-e2e.yaml` instead: the same passwordless
OpenShift login, with image-controller and PaC webhook TLS skip that the e2e suite
needs. That file still has no Dex demo users.

To use a different CR, set `KONFLUX_CR` before running the script:

```bash
KONFLUX_CR=operator/config/samples/konflux-with-github-auth.yaml ./deploy-konflux-on-ocp.sh
```

{{< alert color="warning" >}}
Do not apply the Kind samples (<code>konflux_v1alpha1_konflux.yaml</code>,
<code>konflux-e2e.yaml</code>) on OpenShift. They enable demo users whose password is
published in this repository, and they pin a Kind-specific NodePort. An OpenShift
cluster is usually reachable from the internet, so applying one of them grants
authenticated tenant access to anyone who finds it.
{{< /alert >}}

The script refuses to apply any CR containing `staticPasswords`. If you are working on a
deliberately throwaway cluster and accept the exposure, override it:

```bash
ALLOW_DEV_STATIC_PASSWORDS=true ./deploy-konflux-on-ocp.sh
```

See <a href="{{< relref "../examples" >}}">Examples</a> for alternative sample
configurations and the
[OIDC configuration guide]({{< relref "../guides/oidc-configuration" >}}) for connecting
GitHub, Google, or LDAP.

To swap the CR after the script completes:

```bash
kubectl delete konflux konflux
kubectl apply -f <your-konflux-cr>.yaml
kubectl wait --for=condition=Ready=True konflux konflux --timeout=15m
```

## Uninstall

Remove the Konflux CR and all managed components:

```bash
kubectl delete konflux konflux
```

Remove the operator and CRDs from the `operator/` directory:

```bash
cd operator
make undeploy
make uninstall
```

## What's next

- [GitHub Application Secrets]({{< relref "github-secrets" >}}) — create a GitHub App and configure webhook delivery
- [Onboard a new Application]({{< relref "onboard" >}}) — onboard an application, run builds, tests, and releases
- [Registry Configuration]({{< relref "registry-configuration" >}}) — configure an external container registry for build and release pipelines
- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) — full CR field reference
- [Troubleshooting]({{< relref "../troubleshooting" >}}) — solutions to common issues
- [Examples]({{< relref "../examples" >}}) — sample Konflux CR configurations
