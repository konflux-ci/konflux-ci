Konflux-CI
===

<!-- toc -->

- [Trying Out Konflux](#trying-out-konflux)
  * [Machine Minimum Requirements](#machine-minimum-requirements)
  * [Installing Software Dependencies](#installing-software-dependencies)
  * [Bootstrapping the Cluster](#bootstrapping-the-cluster)
  * [Enable Pipelines Triggering via Webhooks](#enable-pipelines-triggering-via-webhooks)
  * [Onboard a new Application](#onboard-a-new-application)
    + [Option 1: Onboard Application with the Konflux UI](#option-1-onboard-application-with-the-konflux-ui)
      - [Create Application and Component via the Konflux UI](#create-application-and-component-via-the-konflux-ui)
    + [Option 2: Onboard Application with Kubernetes Manifests](#option-2-onboard-application-with-kubernetes-manifests)
      - [Image Registry](#image-registry)
      - [Creating a Pull Request](#creating-a-pull-request)
    + [Observe the Behavior](#observe-the-behavior)
    + [Pull your new Image](#pull-your-new-image)
      - [Public Registry](#public-registry)
      - [Local Registry](#local-registry)
      - [Start a Container](#start-a-container)
    + [Integration Tests](#integration-tests)
      - [Push Builds to External Repository](#push-builds-to-external-repository)
      - [Configure Integration Tests](#configure-integration-tests)
      - [Add Customized Integration Tests (Optional)](#add-customized-integration-tests-optional)
    + [Configure Releases](#configure-releases)
      - [Create the on-push Pipeline](#create-the-on-push-pipeline)
      - [Create Release Resources](#create-release-resources)
      - [Setting up a Tenant Release Pipeline](#setting-up-a-tenant-release-pipeline)
      - [Create a Registry Secret for the Managed Namespace](#create-a-registry-secret-for-the-managed-namespace)
      - [Trigger the Release](#trigger-the-release)
      + [Managed Release Pipelines (optional)](#managed-release-pipelines-optional)
          - [Setting up a Managed Release Pipeline](#setting-up-a-managed-release-pipeline)
          - [Create a Registry Secret for the Managed Namespace](#create-a-registry-secret-for-the-managed-namespace)
          - [Trigger the Managed Release](#trigger-the-managed-release)
  * [Namespace and User Management](#namespace-and-user-management)
    + [Creating a new Namespace](#creating-a-new-namespace)
    + [Granting a User Access to a Namespace](#granting-a-user-access-to-a-namespace)
    + [Add a new User](#add-a-new-user)
  * [Repository Links](#repository-links)

<!-- tocstop -->

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

## Machine Minimum Requirements

The deployment requires the following **free** resources:

**CPU**: 4 cores\
**RAM**: 8 GB

**Note:** Additional load from running multiple pipelines in parallel will require
additional resources.

## Installing Software Dependencies

The following applications are required on the host machine:

* [Kind and kubectl](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
  along with `podman` or `docker`
* `git`
* `openssl`

## Bootstrapping the Cluster

From the root of this repository, run the setup scripts:

1. Create a cluster

```bash
kind create cluster --name konflux --config kind-config.yaml
```

**Note:** If the cluster or any deployments fail to start because of
[too many open files](https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files)
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

1. Start a new channel in [smee](https://smee.io/), and take a note of the webhook
   proxy URL.

2. Create a GitHub app following
   [Pipelines-as-Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup).

   For `Homepage URL` you can insert `https://localhost:9443/` (it doesn't matter).

   For `Webhook URL` insert the smee client's webhook proxy URL from previous steps.

   per the instructions on the link, generate and download the private key and create a
   secret on the cluster providing the location of the private key, the App ID, and the
   openssl-generated secret created during the process.

3. To allow Konflux to send PRs to your application repositories, the same secret should
   be created inside the `build-service` and the `integration-service` namespaces. See
   additional details under
   [Configuring GitHub Application Secrets](./docs/github-secrets.md).

4. Deploy the smee-client on the cluster:

   Edit the [smee-client manifest](./smee/smee-client.yaml), replacing `<smee-channel>`
   with the webhook proxy URL generated when creating the channel.

   Deploy the manifest:

```bash
kubectl create -f ./smee/smee-client.yaml
```

## Onboard a new Application

The next step is to onboard an application to Konflux on behalf of `user2`.

At this point, you have a choice between using the Konflux UI to onboard and using
Kubernetes manifests.

Both options will use an example repository containing a Dockerfile to be built by
Konflux:

1. Fork the [example repository](https://github.com/konflux-ci/testrepo), by clicking
   the `Fork` button from that repository and following the instructions on the "Create
   a new fork" page.

2. Install the GitHub app on your fork: Go to the app's page on GitHub, click on Install
   App on the left-hand side, Select the organization the fork repository is on, click
   `Only select repositories`, and select your fork repository.

We will use our Konflux deployment to build and release Pull Requests for this fork.

### Option 1: Onboard Application with the Konflux UI

With this approach, Konflux can create:
1. The manifests in GitHub for the pipelines it will run against the applications
   onboarded to Konflux.
2. The Quay.io repositories into which it will push container images.

The former is enabled by creating the
[GitHub Application Secrets](./docs/github-secrets.md) **on all 3 namespaces** and
installing your newly-created GitHub app on your repository, as explained above.

The latter is achieved by:
1. Configuring a push secret that will allow the build pipeline to push images to
   Quay.io for namespace `user-ns2`. For that, follow the
   [procedure for configuring the push secret](./docs/quay.md#configuring-a-push-secret-for-the-build-pipeline).
2. Creating an organization and an application in Quay.io that will allow Konflux to
   create repositories for your applications. To do that,
   [Follow the procedure](./docs/quay.md#automatically-provision-quay-repositories-for-container-images)
   to configure a Quay.io application and deploy `image-controller`.

#### Create Application and Component via the Konflux UI

Follow these steps to onboard your application:

1. Login to [Konflux](https://localhost:9443) as `user2`.
2. Click `Create application`
3. Provide a name to the application and click "Add a component"
4. Under `Git repository url`, copy the **https** link to your fork. This should
   be something similar to `https://github.com/<your-name>/testrepo.git`.
5. Leave `Docker file` blank. The default value of `Dockerfile` will be used.
6. Under the Pipeline drop-down list, select `docker-build`.
7. Click `Create application`.

The UI should now display the Lifecycle diagram for your application. In the Components
tab you should be able to see your component listed and you'll be prompted to merge the
automatically-created Pull Request (don't do that just yet).

**NOTE:** if you have NOT completed the Quay.io setup steps in the previous section,
Konflux will be UNABLE to send a PR to your repository. Konflux will display "Sending
Pull Request".

In your GitHub repository you should now see a PR was created with two new pipelines.
One is triggered by PR events (e.g. when PRs are created or changed), and the other is
triggered by push events (e.g. when PRs are merged).

Your application is now onboarded, and you can continue to the
[next step](#observe-the-behavior).

### Option 2: Onboard Application with Kubernetes Manifests

With this approach, we use `kubectl` to deploy the manifests for creating the
`Application` and `Component` resources and we manually create the PR for introducing
the pipelines to run using Konflux.

To do that:

1. Use a text editor to edit your local copy of the
   [example application manifests](./test/resources/demo-users/user/ns2/application-and-component.yaml):

   Under the `Component` and `Repository` resources, change the `url` fields so they
   point to your newly-created fork.

   Note the format differences between the two fields! The `Component` URL has a `.git`
   suffix, while the `Repository` URL doesn't.

   Deploy the manifests:

```bash
kubectl create -f ./test/resources/demo-users/user/ns2/application-and-component.yaml
```
2. Log into the Konflux UI as `user2` (password: `password`). You should be able to see
   your new Application and Component by clicking "View my applications".

#### Image Registry

The build pipeline that you're about to run pushes the image it builds to an image
registry.

For the sake of simplicity, it's configured to use a registry deployed into the
cluster during previous steps of this setup (when dependencies were installed).

**Note:** The statement above is only true when not onboarding via the Konflux UI.
Later in the process, you'll convert it to use a public image registry.

#### Creating a Pull Request

You're now ready to create your first PR to your fork.

1. Clone your fork and create a new branch:

```bash
git clone <my-fork-url>
cd <my-fork-name>
git checkout -b add-pipelines
```

2. Tekton will trigger pipelines present in the `.tekton` directory. The pipelines
   already exist on your repository, you just need to copy them to that location:

```bash
mkdir -p .tekton
cp pipelines/* .tekton/
```

3. Commit your changes and push them to your repository:

```bash
git add .tekton
git commit -m "add pipelines"
git push origin HEAD
```

4. Your terminal should now display a link for creating a new Pull Request in GitHub.
   Click the link, **make sure the PR is targeted against your fork's `main` branch and
   not against the repository from which it was forked** (i.e. `base repository` should
   reside under your user name).

   Finally, click "Create pull request".

### Observe the Behavior

**Note:** If the behavior you see is not as described below, consult the
[troubleshooting document](./docs/troubleshooting.md#pr-changes-are-not-triggering-pipelines).

Once your PR is created, you should see a status is being reported at the bottom of the
PR's comments section (just above the "Add a comment" box).

Your GitHub App should now send PR events to your smee channel. Navigate to your smee
channel's web page. You should see a couple of events were sent just after your PR was
created. E.g. `check_run`, `pull_request`.

Log into the Konflux UI as `user2` and check your applications. Select the application
you created earlier, click on `Activity` and `Pipeline runs`. A build should've been
triggered a few seconds after the PR was created.

Follow the build progress. Depending on your system's load and network connection (the
build process involves pulling images), it might take a few minutes for the build to
complete. It will clone the repository, build using the Dockerfile, and
push the image to the registry.

**Note:** If a pipeline is triggered, but it seems stuck for a long time, especially at
early stages, refer to the troubleshooting document's running out of resources
[section](./docs/troubleshooting.md#running-out-of-resources).

### Pull your new Image

When the build process is done, you can check out the image you just built by pulling it
from the registry.

#### Public Registry

If using a public registry, navigate to the repository URL mentioned in the
`output-image` value of your pull-request pipeline and locate your build.

For example, if using [Quay.io](https://quay.io/repository/), you'd need to go to the
`Tags` tab and locate the relevant build for the tag mentioned on the `output-image`
value (e.g. `on-pr-{{revision}}`), and click the `Fetch Tag` button on the right to
generate the command to pull the image.

#### Local Registry

if using a local registry, Port-forward the registry service, so you can reach it from
outside of the cluster:

```bash
kubectl port-forward -n kind-registry svc/registry-service 30001:80
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

#### Start a Container

Start a container based on the image you pulled:

```bash
podman run --rm be9a47b76264e8fb324d9ef7cddc9...
hello world
```

### Integration Tests

If you onboarded your application using the Konflux UI, the integration tests
are automatically created for you by Konflux.

On the Konflux UI, the integration tests definition should be visible in the
`Integration tests` tab under your application, and a pipeline should've been triggered for them under the `Activity` tab, named after the name of the application. You can
click it and examine the logs to see the kind of things it verifies, and to confirm it passed successfully.

Once confirmed, skip to
[adding customized integration tests](#add-customized-integration-tests-optional).

if you onboarded your application manually, you will now configure your application to
trigger integration tests after each PR build is done.

#### Push Builds to External Repository

**NOTE:** This section is only needed if you did not perform the Quay.io setup steps and
image-controller deployment.

Before you do that, you'll configure your application to use an external registry
instead of the internal one used so far. In order to do that, you'd need to have a
repository, on a public registry, in which you have push permissions.
E.g. [Docker Hub](https://hub.docker.com/), [Quay.io](https://quay.io/repository/):

1. Create an account on a public registry (unless you have one already).

2. Create a [push secret](#configuring-a-push-secret-for-the-build-pipeline) based on
   your login information and deploy it to namespace `user-ns2` on the cluster.

3. Create a new repository on the registry to which your images will be pushed.
   For example, in Quay.io, you'd need to click the
   [Create New Repository](https://quay.io/new/) button and provide it with name and
   location. Free accounts tend to have limits on private repositories, so for the
   purpose of this example, you can make your repository public.

4. Configure your build pipeline to use your new repository on the public registry
   instead of the local registry.

   To do that, edit `.tekton/testrepo-pull-request.yaml` inside your `testrepo` fork
   and replace the value of `output-image` to point to your repository. For example,
   if using Quay.io and your username is `my-user` and you created a repository called
   `my-konflux-component` under your own organization, then the configs should look like this:

```yaml
  - name: output-image
    value: quay.io/my-user/my-konflux-component:on-pr-{{revision}}
```

5. Push your changes to your `testrepo` fork, either as a new PR or as a change to your
   previous PR. Observe the behavior as before, and verify that the build pipeline
   finishes successfully, and that your public repository contains the images pushed by
   the pipeline.

#### Configure Integration Tests

You can add integration tests either via the Konflux UI, or by applying the equivalent
Kubernetes resource.

**NOTE:** If you have imported your component via the UI, a similiar Integration Test is
pre-installed.

In our case, The resource is defined in
`test/resources/demo-users/user/ns2/ec-integration-test.yaml`. You can directly apply
it with the following command:

```bash
kubectl create -f test/resources/demo-users/user/ns2/ec-integration-test.yaml
```

Alternatively, you can provide the content from that YAML using the UI:

1. Login as user2 and navigate to your application and component.

2. Click the `Integration tests` tab.

3. Click `Actions` and select `Add Integration test`.

4. Fill-in the details from the YAML.

5. Click `Add Integration test`.

Either way, you should now see the test listed in the UI under `Integration tests`.

Our integration test is using a pipeline residing in the location defined under the
`resolverRef` field on the YAML mentioned above. From now on, after the build pipeline
runs, the pipeline mentioned on the integration test will also be triggered.

To verify that, go back to your GitHub PR and add a comment: `/retest`.

On the Konflux UI, under your component `Activity` tab, you should now see the build
pipeline running again (`test-component-on-pull-request-...`), and when it's done, you
should see another pipeline run called `test-component-c6glg-...` being triggered.

You can click it and examine the logs to see the kind of things it verifies, and confirm
it passes successfully.

#### Add Customized Integration Tests (Optional)

The integration tests you added just now are relatively generic
[Enterprise Contract](https://enterprisecontract.dev/) tests. The next step adds
a customized test scenario which is specific to our application.

Our simple application is a container image with an entrypoint that prints `hello world`
and exits, and we're going to add a test to verify that it does indeed print that.

An integration test scenario references a pipeline definition. In this case, the
pipeline is defined on our
[example repository](https://github.com/konflux-ci/testrepo/blob/main/integration-tests/testrepo-integration.yaml).
Looking at the pipelines definition, you can see that it takes a single parameter named
`SNAPSHOT`. This parameter is provided automatically by Konflux and it contains
references to the images built by the pipeline that triggered the integration tests.
We can define additional parameters to be passed from Konflux to the pipeline, but in
this case, we only need the snapshot.

The pipeline then uses the snapshot to extract the image that was built by the pipeline
that triggered it and deploys that image. Next, it collects the execution logs and
verifies that they indeed contain `hello world`.

We can either use the Konflux UI or the Kubernetes CLI to add the integration test
scenario.

To add it through the Konflux UI:

1. Login as user2 and navigate to your application and component.

2. Click the `Integration tests` tab.

3. Click `Actions` and select `Add Integration test`.

4. Fill in the fields:

* Integration test name: a name of your choice
* GitHub URL: `https://github.com/konflux-ci/testrepo`
* Revision: `main`
* Path in repository: `integration-tests/testrepo-integration.yaml`

5. Click `Add Integration test`.

Alternatively, you can create it using `kubectl`. The manifest is stored in
`test/resources/demo-users/user/ns2/integration-test-hello.yaml`:

1. Verify the `application` field contains your application name.

2. Deploy the manifest:

```bash
kubectl create -f .test/resources/demo-users/user/ns2/integration-test-hello.yaml
```

You can now post a `/retest` comment on your GitHub PR, and once the `pull-request`
pipeline is done, you should see your new integration test being triggered alongside
the one you had before.

If you examine the logs, you should be able to see the snapshot being parsed and the
test being executed.

### Configure Releases

You will now configure Konflux to release your application to the external registry
configured in previous steps.

This requires:

* A pipeline that will run on push events to the component repository.

* `ReleasePlan` and `ReleasePlanAdmission` resources, that will react on the snapshot to
  be created after the on-push pipeline will be triggered, which, in turn, will trigger
  the creation of the release.

If onboarded using the Konflux UI, the pipeline was already created and configured for
you. Skip to
[creating the release resources](#create-release-resources).

#### Create the on-push Pipeline

You will now configure the on-push pipeline that will be triggered whenever new commits
are created on branch `main` (e.g. when PRs are merged).

Edit the content of the copy you made earlier to the on-push pipeline at
`.tekton/testrepo-push.yaml`, replacing the value of `output-image`, so that the repository URL is identical to the one
[previously set](#push-builds-to-external-repository) for the `pull-request` pipeline.

For example, if using Quay.io and your username is `my-user` and you created a
repository called `my-konflux-component` under your own organization, then the configs
should look like this:

```yaml
  - name: output-image
    value: quay.io/my-user/my-konflux-component:{{revision}}
```

**Note:** this is the same as for the pull request pipeline, but the tag portion now
only includes the revision.

#### Create Release Resources

Once you merge a PR, the on-push pipeline will be triggered and once it completes, a
snapshot will be created and the integration tests will run against the container images
built on the on-push pipeline.

Konflux now needs Release resources that will be used
together with the snapshot for creating a new `Release` resource.

There are 2 modes of processing Releases.

* **Tenant Release Pipelines**
  * These are Release pipelines that are executed within the user or development workspace that are meant to promote artifacts.
* **Managed Release Pipelines**
  * These are Release pipelines that are meant to be executed in a controlled or managed namespace where sensitive credentials are kept.
  * Consider a release process whereby images are published to a corporate registry to be exposed to the public.

Let's focus on Tenant Release pipelines.

##### Setting up a Tenant Release Pipeline

A `Tenant Release Pipeline` simply requires a `ReleasePlan` resource. The `ReleasePlan` resource includes a reference to the application
that a development team wants to release, along with a specification of which `Pipeline` to execute. In addition, `Parameters` may be specified.

The process also requires permissions to be granted to the development environment
`appstudio-pipeline` service account on several resources.

Let's configure the Tenant Release Pipeline.

1. Edit the [release plan](./test/resources/demo-users/user/ns2/release-plan.yaml) and
verify that the `application` field contains the name of your application.

2. Under the components mapping list, set the `name` field such that it matches the name of
   your component and replace `<repository url>` with the URL of the repository on the
   registry to which your released images are to be pushed. This is typically a
   different repository comparing to the one where builds are being pushed during tests.

   For example, if your component is called `test-component`, and you wish to release
   your images to a Quay.io repository called `my-user/my-konflux-component-release`,
   then the configs should look like this:

```yaml
    mapping:
      components:
        - name: test-component
          repository: quay.io/my-user/my-konflux-component-release
```

3. If onboarded not using the UI, you'd need to have the repository created on the
   registry before releases can be pushed to it. See more details on creating
   repositories in [previous steps](#push-builds-to-external-repository).

   If you're using the UI to onboard, the Quay.io application you created will be able
   to create new repositories under that application's organization.

Deploy the Release Plan under the development team namespace (`user-ns2`):

```bash
kubectl create -f ./test/resources/demo-users/user/ns2/tenant-release-plan.yaml
```

#### Trigger the Release

You can now push the changes (if any) to your PR, merge it once the build-pipeline
passes and observe the behavior:

1. Commit the changes you did on your `testrepo` branch (i.e. introducing the on-push
   pipeline, in case you did not onboard via the UI) and push them to GitHub.

2. Once the build-pipeline and the integration tests finish successfully, merge the PR.

3. On the Konflux UI, you should now see your on-push pipeline being triggered.

4. Once it finishes successfully, the integration tests should run once more, and
   a release should be created under the `Releases` tab.

5. Wait for the Release to be complete, and check your registry repository for the
   released image.

**Congratulations**: You just created a release for your application!

Your released image should be available inside the repository pointed by your
`ReleasePlan` resource.

#### Managed Release Pipelines (optional)

##### Setting up a Managed Release Pipeline

A `Managed Release Pipeline` requires both a `ReleasePlan` and a `ReleasePlanAdmission` resource. 
Together, they form the agreement on how and where artifacts should be published.

The `ReleasePlan` resource includes a reference to the application that the development
team wants to release, along with the namespace where the application is supposed to be
released.

The `ReleasePlanAdmission` resource defines how the application should be released, and
it is typically maintained, not by the development team, but by the managed environment
team (the team that supports the deployments of that application).

The `ReleasePlanAdmission` resource makes use of an Enterprise Contract (EC) policy,
which defines criteria for gating releases.

Lastly, the process also requires permissions to be granted to the managed environment
`appstudio-pipeline` service account on several resources.

For more details you can examine the manifests under the
[managed-ns2 directory](./test/resources/demo-users/user/managed-ns2/).

To do all that, follow these steps:

Edit the [managed release plan](./test/resources/demo-users/user/ns2/managed-release-plan.yaml) and
verify that the `application` field contains the name of your application.

Deploy the Release Plan under the development team namespace (`user-ns2`):

```bash
kubectl create -f ./test/resources/demo-users/user/ns2/managed-release-plan.yaml
```

Edit the `ReleasePlanAdmission`
[manifest](./test/resources/demo-users/user/managed-ns2/rpa.yaml):

1. Under `applications`, verify that your application is the one listed.

2. Under the components mapping list, set the `name` field so it matches the name of
   your component and replace `<repository url>` with the URL of the repository on the
   registry to which your released images are to be pushed. This is typically a
   different repository comparing to the one builds are being pushed during tests.

   For example, if your component is called `test-component`, and you wish to release
   your images to a Quay.io repository called `my-user/my-konflux-component-release`,
   then the configs should look like this:

```yaml
    mapping:
      components:
        - name: test-component
          repository: quay.io/my-user/my-konflux-component-release
```

3. If onboarded not using the UI, you'd need to have the repository created on the
   registry before releases can be pushed to it. See more details on creating
   repositories in [previous steps](#push-builds-to-external-repository).

   If you're using the UI to onboard, the Quay.io application you created will be able
   to create new repositories under that application's organization.

Deploy the managed environment team's namespace, along with the resources mentioned
above:

```bash
kubectl create -k ./test/resources/demo-users/user/managed-ns2
```

At this point, you can click **Releases** on the left pane in the UI. The status
for your ReleasePlan should be **"Matched"**.

##### Create a Registry Secret for the Managed Namespace

In order for the release service to be able to push images to the registry, a secret is
needed on the managed namespace (`managed-ns2`). This is the same secret as was
previously created on the development namespace (`user-ns2`).

To do that, follow the instructions for
[creating a push secret for the release pipeline](./docs/quay.md#configuring-a-push-secret-for-the-release-pipeline)
for namespace `managed-ns2`.

##### Trigger the Managed Release

You can now push the changes (if any) to your PR, merge it once the build-pipeline
passes and observe the behavior:

1. Commit the changes you did on your `testrepo` branch (i.e. introducing the on-push
   pipeline, in case you did not onboard via the UI) and push them to GitHub.

2. Once the build-pipeline and the integration tests finish successfully, merge the PR.

3. On the Konflux UI, you should now see your on-push pipeline being triggered.

4. Once it finishes successfully, the integration tests should run once more, and
   a release should be created under the `Releases` tab.

5. Wait for the Release to be complete, and check your registry repository for the
   released image.

**Congratulations**: You just created a managed release for your application!

Your released image should be available inside the repository pointed by your
`ReleasePlanAdmission` resource.

## Namespace and User Management

### Creating a new Namespace

```bash
# Replace $NS with the name of the new namespace

kubectl create namespace $NS
kubectl label namespace "$NS konflux.ci/type=user
kubectl create serviceaccount appstudio-pipeline -n $NS
```

Example:

```bash
kubectl create namespace user-ns3
kubectl label namespace user-ns3 konflux.ci/type=user
kubectl create serviceaccount appstudio-pipeline -n user-ns3
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

## Repository Links

* [Configuring GitHub Secrets](./docs/github-secrets.md)
* [Quay-related Procedures](./docs/quay.md)
* [Troubleshooting common issues](./docs/troubleshooting.md)
* [Release guidelines](./RELEASE.md)
* [Contributing guidelines](./CONTRIBUTING.md)
