Konflux-CI
===

<!-- toc -->

- [Document Conventions](#document-conventions)
- [Trying Out Konflux](#trying-out-konflux)
  * [Operator-Based Deployment](#operator-based-deployment)
    + [Local Development (Kind)](#local-development-kind)
    + [Production Deployment (Any Cluster)](#production-deployment-any-cluster)
    + [Key Differences from Legacy Deployment](#key-differences-from-legacy-deployment)
  * [Machine Minimum Requirements](#machine-minimum-requirements)
  * [Installing Software Dependencies](#installing-software-dependencies)
  * [Bootstrapping the Cluster](#bootstrapping-the-cluster)
  * [Building and Running the Operator from Source](#building-and-running-the-operator-from-source)
  * [Enable Pipelines Triggering via Webhooks](#enable-pipelines-triggering-via-webhooks)
  * [Konflux Tutorial](#konflux-tutorial)
  * [Resource (Memory & CPU) Management](#resource-memory--cpu-management)
    + [Workloads Deployed *with* Konflux](#workloads-deployed-with-konflux)
    + [Workloads Deployed *by* Konflux](#workloads-deployed-by-konflux)
  * [Namespace and User Management](#namespace-and-user-management)
    + [Creating a new Namespace](#creating-a-new-namespace)
    + [Granting a User Access to a Namespace](#granting-a-user-access-to-a-namespace)
    + [Users Management](#users-management)
  * [Repository Links](#repository-links)

<!-- tocstop -->

# Document Conventions

:gear: - **Action Required**: This symbol signifies that the text to follow it requires
the reader to fulfill an action.

# Trying Out Konflux

This section demonstrates the process for deploying Konflux locally, onboarding users
and building and releasing an application. The procedure contains two options for the
user to choose from for onboarding applications to Konflux:

- Using the Konflux UI
- Using Kubernetes manifests

Each of those options has its pros and cons: the procedure described using the UI,
provides more streamlined user experience once setup is done, but it requires using
[Quay.io](https://quay.io) for image registry and requires some additional initial setup
steps comparing to using Kubernetes manifest alone. The latter also supports using any
image registry.

**Note:** The procedure that is described using the UI can also be fulfilled using CLI
and Kubernetes manifests.

In both cases, the recommended way to try out Konflux is using
[Kind](https://kind.sigs.k8s.io/).
The process below creates a Kind cluster using the provided config in this repository.
The config tells Kind to forward port `9443` from the host to the Kind cluster. The port
forwarding is needed for accessing Konflux.

**Note:** If using a remote machine for setup, you'd need to port-forward port `9443` on
the remote machine to port `9443` on your local machine to be able to access the UI from
your local machine.

## Operator-Based Deployment

Konflux can be deployed using the operator-based installer, which supports both local development (Kind) and production deployments on any Kubernetes cluster.

### Local Development (Kind)

For quick local setup on macOS or Linux:

:gear: Create configuration from template:

```bash
cp scripts/deploy-local.env.template scripts/deploy-local.env
```

:gear: Edit `scripts/deploy-local.env` with your GitHub App credentials.
See the template file for instructions on required values.

:gear: Deploy Konflux:

```bash
./scripts/deploy-local.sh
```

**macOS users:** The script handles macOS-specific configuration automatically. See `scripts/deploy-local.env.template` for available options.

This automated script:
- Creates a Kind cluster with proper configuration
- Deploys the Konflux operator
- Applies your Konflux CR configuration
- Sets up GitHub integration
- Provides a local OCI registry at `localhost:5001`

Access Konflux at: https://localhost:9443

### Production Deployment (Any Cluster)

For production deployments on OpenShift, EKS, GKE, or other Kubernetes clusters:

:gear: Install the operator:

```bash
kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/latest/download/install.yaml
```

:gear: Create your Konflux CR based on the [sample configurations](operator/config/samples/):

```bash
kubectl apply -f my-konflux.yaml
```

**Important:** Do not use the sample with demo users (`konflux_v1alpha1_konflux.yaml`) for production - configure OIDC authentication instead.

:gear: Create required secrets (GitHub App, Quay tokens, etc.). See the [Operator Deployment Guide](docs/operator-deployment.md) for details.

### Key Differences from Legacy Deployment

The operator-based deployment differs from the legacy bootstrap approach:

- **Universal:** Works on any Kubernetes cluster, not just Kind
- **Declarative:** Configure via Konflux CR, not shell scripts
- **Production-ready:** Supports HA, custom ingress, and proper secret management
- **Secure defaults:** No demo users in samples (use OIDC connectors)
- **Modular:** Enable only the components you need

For the legacy bootstrap approach (Linux x86_64 only), continue to the sections below.

> [!WARNING]
> The sections below describe the **legacy deployment method** which will be removed in a future release.
> Use the [Operator-Based Deployment](#operator-based-deployment) above for new installations.

## Machine Minimum Requirements

> [!NOTE]
> These requirements apply to the legacy deployment method. The operator-based deployment
> works on macOS, Linux (x86_64 and arm64), and any Kubernetes cluster.

The legacy deployment is currently only supported on **x86_64 Linux** platforms.

The deployment requires the following **free** resources:

**CPU**: 4 cores\
**RAM**: 8 GB

**Note:** Additional load from running multiple pipelines in parallel will require
additional resources.

## Installing Software Dependencies

:gear: Verify that the applications below are installed on the host machine:

* [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) (`v0.26.0` or
  newer) along with `podman` (`v5.3.1` or newer) or `docker` (`v27.0.1` or newer)
* `kubectl` (`v1.31.4` or newer)
* `git` (`v2.46` or newer)
* `openssl` (`v3.0.13` or newer)

## Bootstrapping the Cluster

> [!WARNING]
> This section describes the **legacy manual bootstrap** process.
> For new installations, use [deploy-local.sh](#local-development-kind) instead.

:gear: Clone this repository:

 ```bash
git clone https://github.com/konflux-ci/konflux-ci.git
cd konflux-ci
```

**Note:** It is recommended that you increase the `inotify` resource limits in order to
avoid issues related to
[too many open files](https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files). To increase the limits temporarily, run the
following commands:

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512
```

From the root of this repository, run the setup scripts:

1. :gear: Create a cluster

```bash
kind create cluster --name konflux --config kind-config.yaml
```

**Note:** When using Podman, it is recommended that you increase the PID limit on the
container running the cluster, as the default might not be enough when the cluster
becomes busy:

```bash
podman update --pids-limit 4096 konflux-control-plane
```

**Note:** If pods still fail to start due to missing resources, you may need to reserve
additional resources to the Kind cluster. Edit [kind-config.yaml](./kind-config.yaml)
and modify the `system-reserved` line under `kubeletExtraArgs`:

```yaml
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
        system-reserved: memory=12Gi
```

2. :gear: Deploy the dependencies

Konflux Operator can deploy some of the dependencies (see below). The following command
will skip all dependencies that the operator installs by default:

```bash
SKIP_DEX=true \
SKIP_KONFLUX_INFO=true \
SKIP_CLUSTER_ISSUER=true \
SKIP_INTERNAL_REGISTRY=true \
./deploy-deps.sh
```

> [!NOTE]
> - `SKIP_DEX=true` - Skip Dex deployment (managed by operator for UI component)
> - `SKIP_KONFLUX_INFO=true` - Skip Konflux Info deployment (managed by operator)
> - `SKIP_CLUSTER_ISSUER=true` - Skip Cluster Issuer deployment (managed by operator)
> - `SKIP_INTERNAL_REGISTRY=true` - Skip Internal Registry deployment (managed by operator)

**Note:** If you encounter Docker Hub rate limiting failures during deployment,
see
[Troubleshooting Docker Hub Rate Limits](docs/troubleshooting.md#docker-hub-rate-limits).

3. :gear: Install the Konflux Operator

Install the operator from the latest GitHub release:

```bash
kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/latest/download/install.yaml
```

> [!NOTE]
> To install a specific version instead of the latest, replace `latest` with the version tag:
> ```bash
> kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/download/v0.0.1/install.yaml
> ```

Wait for the operator to be ready:

```bash
kubectl wait --for=condition=Available deployment/konflux-operator-controller-manager -n konflux-operator --timeout=300s
```

4. :gear: Deploy Konflux using the Operator

Create a Konflux Custom Resource to deploy Konflux. This is the **only CR you need** - the operator manages all components from this single resource.

For local testing, you can use the sample configuration with demo users:

```bash
kubectl apply -f <(curl -L \
  https://github.com/konflux-ci/konflux-ci/releases/latest/download/samples.tar.gz | \
  tar -xzO ./konflux_v1alpha1_konflux.yaml)
```

> [!WARNING]
> This sample includes **demo users with insecure static passwords** (user1@konflux.dev / password) for local testing only.
> **Never use this sample for production deployments.** For production, configure OIDC authentication instead.
> See [operator/config/samples/konflux-with-github-auth.yaml](operator/config/samples/konflux-with-github-auth.yaml) for a production example.

> [!NOTE]
> To use a specific version instead of the latest, replace `latest` with the version tag:
> ```bash
> kubectl apply -f <(curl -L \
>   https://github.com/konflux-ci/konflux-ci/releases/download/v0.0.1/samples.tar.gz | \
>   tar -xzO ./konflux_v1alpha1_konflux.yaml)
> ```

Wait for Konflux to be ready:

```bash
kubectl wait --for=condition=Ready=True konflux konflux --timeout=10m
```

> [!NOTE]
> The `deploy-konflux.sh` script is deprecated in favor of the Konflux Operator.
> The operator provides a declarative way to manage Konflux installations and
> enables better lifecycle management, upgrades, and configuration.

5. :gear: Deploy demo users

```bash
./deploy-test-resources.sh
```

6. :gear: If Konflux was installed on a cluster hosted in a remote machine, SSH port-forwarding can
be used to access. Open an additional terminal and run the following command
(make sure to add the details of your remote machine and user):

```bash
ssh -L 9443:localhost:9443 $USER@$VM_IP
```

7. The UI will be available at https://localhost:9443. You can login using a test user.

`username:` `user2@konflux.dev`

`password:` `password`

We now have Konflux up and running. Next, we shall configure Konflux to respond
to Pull Request webhooks, build a user application and push it to a registry.

## Building and Running the Operator from Source

For instructions on building and running the operator from source, see the
[Getting Started](./operator/README.md#getting-started) section in the operator README.

## Enable Pipelines Triggering via Webhooks

Pipelines Can be triggered by Pull Request activities, and their outcomes will be
reported back to the PR page in GitHub.

A GitHub app is required for creating webhooks that Tekton will listen on. When deployed
in a local environment like Kind, GitHub will not be able to reach a service within the
cluster. For that reason, we need to use a proxy that will listen on such events
from within the cluster and will relay those events internally.

To do that, we rely on [smee](https://smee.io/): We configure a GitHub app to send
events to a channel we create on a public `smee` server, and we deploy a client
within the cluster to listen to those events. The client will relay those events to
pipelines-as-code (Tekton) inside the cluster.

When the dependencies were deployed, a smee channel was created for you, a client was
deployed to listen to it, and the channel's webhook Proxy URL was stored in a patch
file.

1. :gear: Take note of the smee channel's webhook Proxy URL created for you:

```
grep value dependencies/smee/smee-channel-id.yaml
```

**NOTE:** if you already have a channel that you'd like to keep using, copy its URL to
the `value` field inside the `smee-channel-id.yaml` file and rerun `deploy-deps.sh`.
The script will not recreate the patch file if it already exists.

2. :gear: Create a GitHub app following
   [Pipelines-as-Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup).

   For `Homepage URL` you can insert `https://localhost:9443/` (it doesn't matter).

   For `Webhook URL` insert the smee client's webhook proxy URL from previous steps.

   :gear: Per the instructions on the link, generate and download the private key and create a
   secret on the cluster providing the location of the private key, the App ID, and the
   openssl-generated secret created during the process.

3. :gear: To allow Konflux to send PRs to your application repositories, the same secret
   should be created inside the `build-service` and the `integration-service`
   namespaces. See additional details under
   [Configuring GitHub Application Secrets](./docs/github-secrets.md).

## Konflux Tutorial

For the complete tutorial on onboarding applications, building container images,
configuring integration tests, and releasing to registries, see the
[Konflux Tutorial](./docs/tutorial.md).
## Resource (Memory & CPU) Management

The Konflux Kind environment is intended to be deployable on workstations, CI runners
and other systems which are not oriented towards Konflux performance. On the other hand,
some of the workloads deployed to the environment were created, tweaked and adjusted
for performance rather than resource conservation.

To run those workloads in Kind, we need to tune them accordingly. This can be done in
a few different ways, mainly depending on whether the workload is created when deploying
the environment (e.g. Konflux core services), or created (and destroyed) while the
system is already active (e.g. Tekton tasks runs).

### Workloads Deployed *with* Konflux

Konflux is deployed using an Operator. Accordingly, resources consumption for its
components is configured through the Konflux Custom Resource rather than by patching
manifests directly. The operator manages these resources declaratively.

To adjust resource consumption, edit your Konflux CR and specify resource `requests`
and `limits` in the component specifications. For example:

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
  # Similar configuration for other components...
```

See the [sample Konflux CR](./operator/config/samples/konflux_v1alpha1_konflux.yaml)
for examples of resource configuration for all components.

For complete API reference documentation, see the
[Konflux Operator API Reference](https://konflux-ci.dev/konflux-ci/docs/reference/).

> [!NOTE]
> If you're using the deprecated `deploy-konflux.sh` script, you can still
> patch installation manifests directly. However, this approach is not recommended as
> it will be overwritten on redeployment. The operator-based approach ensures
> configuration persistence and proper lifecycle management.

### Workloads Deployed *by* Konflux

Workloads created by Konflux may not reside in this repository and may not even be
referenced within it. Also, those workloads can be created and destroyed many times
throughout the lifecycle of the environment, so manipulating the installation manifests
cannot help us with those.

One prominent example is Tekton TaskRuns and the pods those create.

TaskRuns are referenced by PipelineRuns and Pipelines defined by users and Konflux
teams. PipelineRuns reside in user repositories used for onboarding user components.
Some Pipelines used by Konflux can be referenced by `ReleasePlanAdmission` resources
or by core services's configurations.

To be able to run such TaskRuns in our setup, without having to maintain resource-frugal
versions of such tasks and pipelines, we can mutate the pods generated by those tasks
upon their creation.

We use [Kyverno](https://kyverno.io/) to create policies for mutating pods' resource
requests upon their creation. Kyverno will use those policies in order to tune relevant
pods to require less memory and CPU resources in order to start.

In [this example](./dependencies/kyverno/policy/pod-requests-del.yaml), we match against
the `tekton.dev/task` label propagated by Tekton to the pods created for the
`verify-enterprise-contract` Task, and reduce the CPU and memory requests for any pod
created by this Task to a minimum.

In a similar fashion, we can rely on Tekton's
[Automatic Labeling](https://tekton.dev/docs/pipelines/labels/#automatic-labeling)
to match pods by the Tasks and Pipelines that create them, and then mutate them
according to our needs and limitations.

## Namespace and User Management

### Creating a new Namespace

```bash
# Replace $NS with the name of the new namespace

kubectl create namespace $NS
kubectl label namespace "$NS konflux-ci.dev/type=tenant
```

Example:

```bash
kubectl create namespace user-ns3
kubectl label namespace user-ns3 konflux-ci.dev/type=tenant
```

### Granting a User Access to a Namespace

```bash
# Replace $RB with the name of the role binding (you can choose the name)
# Replace $USER with the email address of the user
# Replace $NS with the name of the namespace the user should access

kubectl create rolebinding $RB --clusterrole konflux-admin-user-actions --user $USER -n $NS
```

Example:

```bash
kubectl create rolebinding user1-konflux --clusterrole konflux-admin-user-actions --user user1@konflux.dev -n user-ns3
```

### Users Management

[Dex](https://dexidp.io/) is used for integrating identity providers into Konflux.
Together with [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy), it allows
for offloading authentication to different identity providers per the requirement
of the environment or organization where Konflux is installed.

The Konflux Operator, manages the Dex configurations through the Konflux
Custom Resource. For the simple standalone deployment, static passwords are configured
in the `spec.ui.spec.dex.config` section of the Konflux CR. See the
[sample Konflux CR](./operator/config/samples/konflux_v1alpha1_konflux.yaml) for an
example configuration with static users.

To add or modify users, edit your Konflux CR and update the `staticPasswords` section:

```yaml
spec:
  ui:
    spec:
      dex:
        config:
          enablePasswordDB: true
          passwordConnector: local
          staticPasswords:
          - email: "user1@konflux.dev"
            hash: "$2a$10$..." # bcrypt hash of the password
            username: "user1"
            userID: "7138d2fe-724e-4e86-af8a-db7c4b080e20"
```

After updating the CR, the operator will reconcile the changes and update the Dex
configuration accordingly.

> [!NOTE]
> If you're using the deprecated `deploy-konflux.sh` script, Dex configuration
> is defined in [dependencies/dex/config.yaml](./dependencies/dex/config.yaml). However,
> this approach is not recommended as manual edits will be overwritten on redeployment.

For advanced authentication scenarios, see Dex documentation for both
[OAuth 2.0](https://dexidp.io/docs/connectors/oauth/) and the
[builtin connector](https://dexidp.io/docs/connectors/local/).

For an example of configuring GitHub authentication, see the
[sample Konflux CR with GitHub authentication](./operator/config/samples/konflux-with-github-auth.yaml).

## Repository Links

* [Configuring GitHub Secrets](./docs/github-secrets.md)
* [Registry Configuration](./docs/registry-configuration.md)
* [Konflux Tutorial](./docs/tutorial.md)
* [Troubleshooting common issues](./docs/troubleshooting.md)
* [Release Process](./RELEASE.md)
* [Contributing guidelines](./CONTRIBUTING.md)
