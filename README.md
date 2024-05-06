# Konflux-CI
Integration and release of Konflux-CI

## Trying Out Konflux

The recommended way to try out Konflux is using [Kind](https://kind.sigs.k8s.io/).
Create a Kind cluster using the provided config in this repository.
The config tells Kind to forward port `9443` from the host to the Kind cluster. The port
forwarding is needed for accessing Konflux.

### Machine requirements

The deployment requires 8GB of free RAM.

### Installing the dependencies

Install [Kind and kubectl](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)

Install `openssl`

### Bootstrapping the cluster

From the root of this repository, run the following commands:

1. Create a cluster

```bash
kind create cluster --name konflux --config kind-config.yaml
```

**Note:** If the cluster fails to start because of [too many open files](https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files)
run the following commands:

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512
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

3. Deploy the dependencies

```bash
./deploy-deps.sh
```

4. Deploy Konflux

```bash
./deploy-konflux.sh
```

5. Deploy demo users

```bash
./deploy-test-resources.sh
```

6. The UI will be available at https://localhost:9443. You can login using the test user.

`username:` `user1`

`password:` `password`

## Configuring Secrets

### Github Application

- build-service github app (global or namespace) - TBA
- integration-service github app - TBA

### Configuring a push secret for the build pipeline

After the build-pipeline builds an image, it will try to push it to a container registry.
The namespace where the pipeline is running should be configured with a push secret for
the container registry.

Tekton provides a way to inject push secrets into pipelines by attaching them to a
service account.

The service account used for running the pipelines is the namespace's `default` service
account.

1. Create the secret in the pipeline's namespace (see example below for extracting the
   secret):

```bash
kubectl create -n $NS secret generic regcred \
 --from-file=.dockerconfigjson=<path/to/.docker/config.json> \
 --type=kubernetes.io/dockerconfigjson
```

2. Add the secret to the namespace's default service account

```bash
kubectl patch -n $NS serviceaccount default -p '{"secrets": [{"name": "regcred"}]}'
```

#### Example - extract Quay push secret:

If using Quay.io, you can follow the procedure below to obtain the config.json file used
for creating the secret. If not using quay, apply your registry's equivalent procedure.

1. Log into quay.io and click your user icon on the top-right corner.

2. Select Account Settings.

3. Click on Generate Encrypted Password.

4. Enter your login password and click Verify.

5. Select Docker Configuration.

6. Click Download `<your-username>-auth.json` and take note of the download location.

7. Replace `<path/to/.docker/config.json>` with this path.

### Configuring a push secret for the release pipeline

If the release pipeline used need to push image to a container
registry, it needs to be configured with a push secret as well.

In the `managed` namespace, repeat the same steps mentioned
[above](#configuring-a-push-secret-for-the-build-pipeline) for
configuring the push secret.

## Using Konflux

### Create an Application and Component

Application and Component resources are required to allow Konflux to track user builds
and releases. A Repository resource is required for triggering CI on PR changes
(webhooks).

Those resources currently need to be created directly in Kubernetes.
[Example manifests](./test/resources/demo-users/user/ns2/application-and-component.yaml)
are available for creating such resources. Modify them per your requirements, setting
the relevant user namespace and repository URL.

### Create Integration test for your application

TBA

### Configure a release pipeline

TBA

### Enable Pipelines Triggering via Webhooks

Pipelines Can be triggered by Pull Request activities, and their outcomes will be
reported back to the PR page in GitHub.

A GitHub app is required for creating webhooks that Tekton will listen on. When deployed
in a local environment like Kind, GitHub will not be able to reach a service within the
cluster. For that reason, we need to use a proxy service that will listen on such events
from within the cluster and will relay those events internally.

To do that, we rely on [smee](https://smee.io/): We configure a GitHub app to send
events to a public channel we create on a public `smee` server, and we deploy a client
within the cluster to listen to those events. The client will rely those events to
the pipelines-as-code (Tekton) inside the cluster.

1. Create a new channel in [smee](https://smee.io/), and take a note of the webhook
   proxy URL.

2. Create a GitHub app following
   [Pipelines-as-Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup).

   For `Homepage URL` you can insert `https://localhost:9443/` (it doesn't matter).

   For `Webhook URL` insert the smee client's webhook proxy URL from previous step.

   Generate and download the private key and create a secret on the cluster per the
   instructions, providing the location of the private key, the App ID, and the
   openssl-generated secret created during the process.

3. Install the app on the repository: Go to the app page on GitHub, click on Install App
   on the left-hand side. Select the organization the component repository is on, click
   `Only select repositories`, and select your component's repository.

4. Deploy the smee-client on the cluster: Edit the
   [smee-client manifest](./smee/smee-client.yaml), replacing `<smee-channel>` with the
   webhook proxy URL generated when creating the channel.

5. Once
   [Application, component and Repository resources](#create-an-application-and-component)
   are present for the repository, creating a Pull Request on the repository should
   trigger, within the cluster, any Tekton `PipelineRun` defined under the repository's
   `.tekton` directory, and configured to be triggered on `pull_request` events (see
   [reference pipeline](./test/resources/demo-users/user/ns2/pull-request.yaml) for more
   details).

   The pipeline's execution should also be visible then in the Konflux UI.

   **Note:** If the pipeline also pushes images to a registry, a registry push secret
   [should be configured](#configuring-a-push-secret-for-the-build-pipeline) for the
   build pipeline.

## Namespace and user management

### Creating a new namespace

```bash
# Replace $NS with the name of the new namespace

kubectl create namespace $NS
kubectl label namespace "$NS konflux.ci/type=user
```

Example:

```bash
kubectl create namespace user-ns3
kubectl label namespace user-ns3 konflux.ci/type=user
```

### Granting a user access to a namespace

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

### Add a new user

Konflux is using [Keycloak](https://www.keycloak.org/) for managing users and
authentication.
The administration console for Keycloak is exposed at
https://localhost:9443/idp/admin/master/console/#/redhat-external

For getting the username and password for the console run:

```bash
# USERNAME

kubectl get -n keycloak secrets/keycloak-initial-admin --template={{.data.username}} | base64 -d

# PASSWORD

kubectl get -n keycloak secrets/keycloak-initial-admin --template={{.data.password}} | base64 -d
```

After login into the console, click on the `Users` tab
on the left for adding a user.

In addition, you can configure additional `Identity providers` such as `Github`,
`Google`, etc.. by clicking on the `Identity providers` tab on the left.
