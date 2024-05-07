# Konflux-CI

Integration and release of Konflux-CI

## Trying Out Konflux

This section demonstrates the process of deploying Konflux locally, onboarding users and
building and releasing an application. This procedure emphasizes streamlined deployment.
Once you have it running, consult later sections for additional integration and
configuration options.

The recommended way to try out Konflux is using [Kind](https://kind.sigs.k8s.io/).
Create a Kind cluster using the provided config in this repository.
The config tells Kind to forward port `9443` from the host to the Kind cluster. The port
forwarding is needed for accessing Konflux.

### Machine Requirements

The deployment requires 8GB of free RAM.

### Installing Software Dependencies

The following applications are required on the host machine:

* [Kind and kubectl](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
  along with `podman` or `docker`
* `git`
* `openssl`

### Bootstrapping the Cluster

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

2. Deploy the dependencies

```bash
./deploy-deps.sh
```

3. Deploy Konflux

```bash
./deploy-konflux.sh
```

4. Deploy demo users

```bash
./deploy-test-resources.sh
```

5. The UI will be available at https://localhost:9443. You can login using the test user.

`username:` `user1`

`password:` `password`

We now have Konflux up and running. Next, we shall configure Konflux to respond
to Pull Request webhooks, build a user application and push it to a registry.

### Enable Pipelines Triggering via Webhooks

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

1. Start a new channel in [smee](https://smee.io/), and take a note of the webhook
   proxy URL.

2. Create a GitHub app following
   [Pipelines-as-Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup).

   For `Homepage URL` you can insert `https://localhost:9443/` (it doesn't matter).

   For `Webhook URL` insert the smee client's webhook proxy URL from previous steps.

   Generate and download the private key and create a secret on the cluster per PaC
   instructions, providing the location of the private key, the App ID, and the
   openssl-generated secret created during the process.

3. Deploy the smee-client on the cluster:

   Edit the [smee-client manifest](./smee/smee-client.yaml), replacing `<smee-channel>`
   with the webhook proxy URL generated when creating the channel.

   Deploy the manifest:

```bash
kubectl create -f ./smee/smee-client.yaml
```

### Onboard a new Application

We now create a new GitHub repository for our application and set it up in Konflux:

1. Fork the
   [example repository](https://github.com/konflux-ci/testrepo), by
   clicking the Fork button from that repository and following the instructions on the
   "Create a new fork" page.

   We will use our Konflux deployment to build and release Pull Requests for this fork.

2. Use a text editor to edit your local copy of the
   [example application manifests](./test/resources/demo-users/user/ns2/application-and-component.yaml):

   Under the `Component` and `Repository` resources, change the `url` fields so they
   point to your newly-created fork.

   Note the format differences between the two fields! The `Component` URL has a `.git`
   suffix, while the `Repository` URL doesn't.

   Deploy the manifests:

```bash
kubectl create -f ./test/resources/demo-users/user/ns2/application-and-component.yaml
```

**Note:** Further explanation about those resources can be found in
[later sections](#create-application-and-component).

3. Log into the Konflux UI as `user2` (password: `password`). You should be able to see
   your new Application and Component by clicking "View my applications".

4. Install the GitHub app on your fork: Go to the app's page on GitHub, click on Install
   App on the left-hand side, Select the organization the fork repository is on, click
   `Only select repositories`, and select your fork repository.

### Image Registry

The pipeline that we're about to run pushes the image it builds to an image registry.

For the sake of simplicity, it's configured to use a registry deployed into the
cluster during previous steps of this setup (when dependencies were installed).

See [next steps](#next-steps) for having your pipelines use registries outside of the
cluster.

### Creating a Pull Request

We're now ready to create our first PR to our fork.

1. Clone your fork and create a new branch:

```bash
git clone <my-fork-url>
cd <my-fork-name>
git checkout -b build-pipeline
```

2. Modify the build-pipeline definition to point to your fork repository. The
   build-pipeline is defined in: `.tekton/testrepo-pull-request.yaml`.

   Edit the `git-url` field so that it points to your repository.

```yaml
- name: git-url
   value: https://github.com/<my-org>/<my-repo>.git
```

3. Commit your changes and push them to your repository:

```bash
git add .tekton/testrepo-pull-request.yaml
git commit -m "set the build-pipeline"
git push origin HEAD
```

4. Your terminal should now display a link for creating a new Pull Request in GitHub.
   Click the link, make sure the PR is targeted against your fork's `main` branch and
   not against the repository from which it was forked (i.e. `base repository` should
   reside under your user name).

   Finally, click "Create pull request".

### Observe the Behavior

Once your PR is created, you should see a status is being reported at the bottom of the
PR's comments section (just above the "Add a comment" box).

Your GitHub App should now send PR events to your smee channel. Navigate to your smee
channel's web page. You should see `issue_comment` and `check_run` events were sent just
after you created the PR.

Log into the Konflux UI as `user2` and check your applications. Select the application
you created earlier, click on `Activity` and `Pipeline runs`. A build should be triggered
a few seconds after the PR was created/changed.

Follow the build progress. Depending on your system's load and network connection (the
build process involves pulling images), it might take a few minutes for the build to
complete. It will clone the repository, build using the Dockerfile, and
push the image to the registry.

If a build is not starting or if you're running into troubles, consult the
[troubleshooting section](#troubleshooting-common-issues).

### Pull your new Image

When the build process is done, you can check out the image you just built by pulling it
from the registry.

Port-forward the registry service, so you can reach it from outside of the cluster:

```bash
kubectl port-forward -n kind-registry svc/registry-service 30001:5001
```

Leave the terminal hanging and on a new terminal window:

List the repositories on the registry:

```bash
curl http://localhost:30001/v2/_catalog
```

The output should look like this:

```bash
{"repositories":["test-component"]}
```

You can now list the tags on that `test-component` repository (assuming you did not
change the pipeline's output-image parameter):

```bash
curl http://localhost:30001/v2/test-component/tags/list
```

You should see a list of tags pushed to that repository. Take a note of that.

```bash
{"name":"test-component","tags":["on-pr-1ab9e6d756fbe84aa727fc8bb27c7362d40eb3a4","sha256-b63f3d381f8bb2789f2080716d88ed71fe5060421277746d450fbcf938538119.sbom"]}
```

Pull the image starting with `on-pr-` (we use `podman` below, but the commands should be
similar on `docker`):

```bash
podman pull --tls-verify=false localhost:30001/test-component:on-pr-1ab9e6d756fbe84aa727fc8bb27c7362d40eb3a4
Trying to pull localhost:30001/test-component:on-pr-1ab9e6d756fbe84aa727fc8bb27c7362d40eb3a4...
Getting image source signatures
Copying blob cde118a3f567 done   |
Copying blob 2efec45cd878 done   |
Copying blob fd5d635ec9b7 done   |
Copying config be9a47b762 done   |
Writing manifest to image destination
be9a47b76264e8fb324d9ef7cddc93a933630695669afc4060e8f4c835c750e9
```

Start a container based on the image you pulled:

```bash
podman run be9a47b76264e8fb324d9ef7cddc93a933630695669afc4060e8f4c835c750e9
hello world
hello world
...
```

### Next Steps

The procedure above is intentionally simplified. Things you can do next to make it more
exhaustive:

* Onboard your own application.

* Enable your pipeline to push to a public registry service by configuring a
  [push secret](#configuring-a-push-secret-for-the-build-pipeline).

## Configuring Secrets

### Github Application

- build-service github app (global or namespace) - TBA
- integration-service github app - TBA

### Configuring a Push Secret for the Build Pipeline

After the build-pipeline builds an image, it will try to push it to a container registry.
If using a registry that requires authentication, the namespace where the pipeline is
running should be configured with a push secret for the registry.

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

#### Example - Extract Quay Push Secret:

If using Quay.io, you can follow the procedure below to obtain the config.json file used
for creating the secret. If not using quay, apply your registry's equivalent procedure.

1. Log into quay.io and click your user icon on the top-right corner.

2. Select Account Settings.

3. Click on Generate Encrypted Password.

4. Enter your login password and click Verify.

5. Select Docker Configuration.

6. Click Download `<your-username>-auth.json` and take note of the download location.

7. Replace `<path/to/.docker/config.json>` with this path.

### Configuring a Push Secret for the Release Pipeline

If the release pipeline used need to push image to a container registry, it needs to be
configured with a push secret as well.

In the `managed` namespace, repeat the same steps mentioned
[above](#configuring-a-push-secret-for-the-build-pipeline) for configuring the push
secret.

## Using Konflux

### Create Application and Component

Application and Component resources allow Konflux to track user builds and releases.
A Repository resource is required for triggering CI on PR changes (webhooks).

Those resources currently need to be created directly in Kubernetes.
[Example manifests](./test/resources/demo-users/user/ns2/application-and-component.yaml)
are available for creating such resources. Modify them per your requirements, setting
the relevant user namespace and repository URL.

### Create Integration Test for your Application

TBA

### Configure a Release Pipeline

TBA

## Namespace and User Management

### Creating a new Namespace

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

### Add a new User

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

## Troubleshooting Common Issues

### PR changes are not Triggering Pipelines

Follow this procedure if you create a PR or make changes to a PR and a pipeline is not
triggered:

1. Confirm that events were logged to the smee channel. if not, verify your steps for
   setting up the GitHub app and installing the app to your fork repository.

2. Confirm that events are being relayed by the smee client. List the pods under the
   `smee-client` namespace and check the logs of the pod on the namespace. Those should
   have mentions of the channel events being forwarded to pipelines-as-code.

```bash
kubectl get pods -n smee-client
kubectl logs -n smee-client gosmee-client-<some-id>
```

3. If the pod is not there or the logs do not include the mentioned entries, confirm you
   properly set the **smee channel** on the smee-client manifest and that you deploy the
   manifest to your cluster.

```bash
kubectl delete -f ./smee/smee-client.yaml
<fix the manifests>
kubectl create -f ./smee/smee-client.yaml
```

**Note:** if the host machine goes to sleep mode, the smee client might stop responding
to events on the smee channel, once the host is up again. This can be addressed by
deleting the client pod and waiting for it to be recreated:

```bash
kubectl get pods -n smee-client
kubectl delete pods -n smee-client gosmee-client-<some-id>
```

4. Check the pipelines-as-code logs to see that events are being properly linked to the
   Repository resource. If you see log entries mentioning a repository resource cannot
   be found, compare the repository mentioned on the logs to the one deployed when
   creating the application and component resources. Fix the Repository resource
   manifest and redeploy it.

```bash
kubectl get pods -n pipelines-as-code
kubectllogs -n pipelines-as-code pipelines-as-code-controller-<some-id>
<fix the manifests>
kubectl apply -f ./test/resources/demo-users/user/ns2/application-and-component.yaml
```

5. On the PR page, type `/retest` on the comment box and post the comment. Observe the
   behavior once more.

### Setup Scripts or Pipeline Execution Fail

If setup scripts fail or pipelines are stuck or tend to fail at relatively random
stages, it might be that the cluster is running out of resources.

That could be:

* Open files limit.
* Open threads limit.
* Cluster running out of memory.

For mitigation steps, consult the notes at the top of the
[cluster setup section](#bootstrapping-the-cluster).

As last resort, you could restart the container running the cluster node. If you do
that, you'd have to, once more, increase the PID limit for that container as explained
in the [cluster setup section](#bootstrapping-the-cluster).

To restart the container ( if using Docker, replace `podman` with `docker`):

```bash
podman restart konflux-control-plane
```

**Note:** It might take a few minutes for the UI to become available once the container
is restarted.
