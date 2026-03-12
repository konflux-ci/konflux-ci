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
| [Go](https://go.dev/) | v1.25.0 |
| [openssl](https://www.openssl.org/) | v3.0.13 |

- `cluster-admin` permissions

{{< alert color="warning" >}}
The script skips the Smee webhook proxy (<code>SKIP_SMEE=true</code>). For GitHub
to deliver webhook events (triggering build pipelines on pull requests), your cluster
must be reachable from the internet. If it is not, you will need to configure Smee
manually after installation. See
<a href="{{< relref "github-secrets" >}}">GitHub Application Secrets</a> for details.
{{< /alert >}}

## Setup

1. Clone the repository:

```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci
```

2. Run the deployment script:

```bash
./deploy-konflux-on-ocp.sh
```

The script performs all of the following automatically:

- Deploys Konflux dependencies using OpenShift-native operators
- Installs the Konflux CRDs
- Deploys the Konflux Operator into the `konflux-operator` namespace
- Waits for the Operator to be ready
- Applies the default Konflux CR and waits for all components to reach `Ready`

## What gets deployed

### Dependencies

| Component | Details |
|-----------|---------|
| OpenShift Pipelines | Installed via OLM (Red Hat's productized Tekton) |
| cert-manager | Installed via the Red Hat cert-manager OLM operator |
| trust-manager | Deployed into the `cert-manager` namespace |
| Kyverno | Policy engine for namespace and RBAC automation |
| Pipelines-as-Code | GitHub-triggered pipeline automation |
| Tekton Chains RBAC | RBAC for supply-chain signing using OpenShift namespaces |

The following components are not deployed by `deploy-deps.sh` in this configuration:

| Skipped | Reason |
|---------|--------|
| Dex | Managed by the Konflux Operator as part of the Konflux CR reconciliation |
| Internal OCI registry | OpenShift has its own integrated registry |
| Smee webhook proxy | Not needed when the cluster is internet-reachable |

### Operator and Konflux

| Component | Details |
|-----------|---------|
| Konflux CRDs | `Konflux` custom resource definition |
| Konflux Operator | Deployed in the `konflux-operator` namespace |
| Konflux instance | All Konflux components managed by the default sample CR |

## Verify the installation

Check the Konflux CR status:

```bash
kubectl get konflux konflux
```

When Konflux is ready, the output includes the UI URL:

```
NAME      READY   UI-URL                                                    AGE
konflux   True    https://konflux-ui-konflux-ui.apps.<cluster-domain>       10m
```

Wait for the `Ready` condition if the deployment is still in progress:

```bash
kubectl wait --for=condition=Ready=True konflux konflux --timeout=15m
```

## Configuration

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

### Konflux CR

The script applies `operator/config/samples/konflux_v1alpha1_konflux.yaml` by default.

{{< alert color="warning" >}}
The default CR contains demo users with static passwords intended for local testing
only. Never use this configuration in a production environment. Use OIDC authentication
instead. See <a href="{{< relref "../examples" >}}">Examples</a> for alternative sample
configurations.
{{< /alert >}}

To use a different CR, apply it after the script completes:

```bash
kubectl delete konflux konflux
kubectl apply -f <your-konflux-cr>.yaml
kubectl wait --for=condition=Ready=True konflux konflux --timeout=15m
```

## Create GitHub integration secrets

After the script completes, create the Pipelines-as-Code secret in the namespaces
that require it:

```bash
for ns in pipelines-as-code build-service integration-service; do
  kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
    --from-file=github-private-key=/path/to/github-app.pem \
    --from-literal=github-application-id="<your-app-id>" \
    --from-literal=webhook.secret="<your-webhook-secret>"
done
```

See [GitHub Application Secrets]({{< relref "github-secrets" >}}) for full instructions
on creating a GitHub App and configuring webhook delivery.

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
