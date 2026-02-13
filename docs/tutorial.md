Konflux Tutorial
===

This document will walk you through some of the common operations in Konflux
including onboarding repositories, building, testing, and releasing artifacts.

*NOTE:* All commands shown in this document assume you are in the repository root.

<!-- toc -->

- [Onboard a new Application](#onboard-a-new-application)
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
      - [Configure Integration Tests](#configure-integration-tests)
      - [Add Customized Integration Tests (Optional)](#add-customized-integration-tests-optional)
    + [Configure Releases](#configure-releases)
      - [Create ReleasePlan and ReleasePlanAdmission Resources](#create-releaseplan-and-releaseplanadmission-resources)
      - [Create a Registry Secret for the Managed Namespace](#create-a-registry-secret-for-the-managed-namespace)
      - [Trigger the Release](#trigger-the-release)
    + [Working with External Image Registry (Optional)](#working-with-external-image-registry-optional)
      - [Push Pull Request Builds to External Registry](#push-pull-request-builds-to-external-registry)
      - [Use External Registry for on-push Pipeline](#use-external-registry-for-on-push-pipeline)

<!-- tocstop -->

# Onboard a new Application

The next step is to onboard an application to Konflux on behalf of user2.

This section includes two options for onboarding an application to Konflux.

The first option demonstrates using the Konflux UI to onboard an application and
releases its builds to quay.io.

The second option demonstrates using Kubernetes manifests to onboard, and releases
the builds to a container registry deployed to the cluster. The idea behind this
scenario is to simplify onboarding in order to demonstrate Konflux with greater ease.

Both options will use an example repository containing a Dockerfile to be built by
Konflux:

1. :gear: Fork the [example repository](https://github.com/konflux-ci/testrepo), by
   clicking the `Fork` button from that repository and following the instructions on the
   "Create a new fork" page.

2. :gear: Install the GitHub app on your fork: Go to the app's page on GitHub, click on
   Install App on the left-hand side, Select the organization the fork repository is on,
   click `Only select repositories`, and select your fork repository.

We will use our Konflux deployment to build and release Pull Requests for this fork.

### Option 1: Onboard Application with the Konflux UI

With this approach, Konflux can create:
1. The manifests in GitHub for the pipelines it will run against the applications
   onboarded to Konflux.
2. The Quay.io repositories into which it will push container images.

The former is enabled by creating the
[GitHub Application Secrets](./github-secrets.md) **on all 3 namespaces** and
installing your newly-created GitHub app on your repository, as explained above.

To achieve the latter follow the step below:

:gear: Create an organization and an application in Quay.io that will allow Konflux to
create repositories for your applications. To do that,
[Follow the procedure](./registry-configuration.md#automatic-repository-provisioning-quayio)
to configure a Quay.io application and deploy `image-controller`.

#### Create Application and Component via the Konflux UI

:gear: Follow these steps to onboard your application:

1. Login to [Konflux](https://localhost:9443) as `user2@konflux.dev` (password:
   `password`).
2. Click `Create application`
3. Verify the workspace is set to `user-ns2` (notice the `ws` breadcrumb trail just
   above `Create an application` and click the `...` to switch workspaces as needed).
4. Provide a name to the application and click "Add a component"
5. Under `Git repository url`, copy the **https** link to your fork. This should
   be something similar to `https://github.com/<your-name>/testrepo.git`.
6. Uncheck the "Should the image produced be private" checkbox. If left checked, Pull Request
   pipelines will not start because image-controller will fail to provision the image
   repository.
7. Leave `Docker file` blank. The default value of `Dockerfile` will be used.
8. Under the Pipeline drop-down list, select `docker-build-oci-ta`.
9. Click `Create application`.

**NOTE:** If you encounter `404 Not Found` error, refer to the
[troubleshooting guide](./troubleshooting.md#unable-to-create-application-with-component-using-the-konflux-ui).

The UI should now display the Lifecycle diagram for your application. In the Components
tab you should be able to see your component listed and you'll be prompted to merge the
automatically-created Pull Request (don't do that just yet. we'll have it merged in
section [Trigger the Release](#trigger-the-release)).

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

1. :gear: Use a text editor to edit your local copy of the
   [example application manifests](../test/resources/demo-users/user/sample-components/ns2/application-and-component.yaml):

   Under the `Component` and `Repository` resources, change the `url` fields so they
   point to your newly-created fork.

   Note the format differences between the two fields! The `Component` URL has a `.git`
   suffix, while the `Repository` URL doesn't.

   Deploy the manifests:

```bash
kubectl create -f ./test/resources/demo-users/user/sample-components/ns2/application-and-component.yaml
```
2. :gear: Log into the Konflux UI as `user2@konflux.dev` (password: `password`). You
   should be able
   to see your new Application and Component by clicking "View my applications".

#### Image Registry

The build pipeline that you're about to run pushes the images it builds to an image
registry.

For the sake of simplicity, it's configured to use a registry deployed into the
cluster during previous steps of this setup (when dependencies were installed).

**Note:** The statement above is only true when not onboarding via the Konflux UI.
You can convert it to use a public image registry later on.

#### Creating a Pull Request

You're now ready to create your first PR **to your fork**.

1. :gear: Clone your fork and create a new branch:

```bash
git clone <my-fork-url>
cd <my-fork-name>
git checkout -b add-pipelines
```

2. Tekton will trigger pipelines present in the `.tekton` directory. The pipelines
   already exist on your repository, you just need to copy them to that location.

   :gear: Copy the manifests:

```bash
mkdir -p .tekton
cp pipelines/* .tekton/
```

3. :gear: Commit your changes and push them to your repository:

```bash
git add .tekton
git commit -m "add pipelines"
git push origin HEAD
```

4. :gear: Your terminal should now display a link for creating a new Pull Request in
   GitHub. Click the link, **make sure the PR is targeted against your fork's `main`
   branch and not against the repository from which it was forked** (i.e.
   `base repository` should reside under your user name).

   Finally, click "Create pull request" (we'll have it merged in section
   [Trigger the Release](#trigger-the-release)).

### Observe the Behavior

**Note:** If the behavior you see is not as described below, consult the
[troubleshooting document](./troubleshooting.md#pr-changes-are-not-triggering-pipelines).

Once your PR is created, you should see a status is being reported at the bottom of the
PR's comments section (just above the "Add a comment" box).

Your GitHub App should now send PR events to your smee channel. Navigate to your smee
channel's web page. You should see a couple of events were sent just after your PR was
created. E.g. `check_run`, `pull_request`.

:gear: Log into the Konflux UI as user2 and check your applications. Select the
application you created earlier, click on `Activity` and `Pipeline runs`. A build
should've been triggered a few seconds after the PR was created.

Follow the build progress. Depending on your system's load and network connection (the
build process involves pulling images), it might take a few minutes for the build to
complete. It will clone the repository, build using the Dockerfile, and
push the image to the registry.

**Note:** If a pipeline is triggered, but it seems stuck for a long time, especially at
early stages, refer to the troubleshooting document's running out of resources
[section](./troubleshooting.md#running-out-of-resources).

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

:gear: If using a local registry, Port-forward the registry service, so you can reach it
from outside of the cluster:

```bash
kubectl port-forward -n kind-registry svc/registry-service 30001:443
```

The local registry is using a self-signed certificate that is being distributed to all
namespaces. You can fetch the certificate from the cluster and use it on the `curl`
calls below. This will look something like this:

```bash
kubectl get secrets -n kind-registry local-registry-tls \
   -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt

curl --cacert ca.crt https://...
```

Instead, we're going to use the `-k` flag to skip the TLS verification.

Leave the terminal hanging and on a new terminal window:

:gear: List the repositories on the registry:

```bash
curl -k https://localhost:30001/v2/_catalog
```

The output should look like this:

```bash
{"repositories":["test-component"]}
```

:gear: List the tags on that `test-component` repository (assuming you did not
change the pipeline's output-image parameter):

```bash
curl -k https://localhost:30001/v2/test-component/tags/list
```

You should see a list of tags pushed to that repository. Take a note of that.

```bash
{"name":"test-component","tags":["on-pr-1ab9e6d756fbe84aa727fc8bb27c7362d40eb3a4","sha256-b63f3d381f8bb2789f2080716d88ed71fe5060421277746d450fbcf938538119.sbom"]}
```

:gear: Pull the image starting with `on-pr-` (we use `podman` below, but the commands
should be similar on `docker`):

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

:gear: Start a container based on the image you pulled:

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

#### Configure Integration Tests

You can add integration tests either via the Konflux UI, or by applying the equivalent
Kubernetes resource.

**NOTE:** If you have imported your component via the UI, a similar Integration Test is
pre-installed.

In our case, the resource is defined in
`test/resources/demo-users/user/sample-components/ns2/ec-integration-test.yaml`.

:gear: Apply the resource manifest:

```bash
kubectl create -f ./test/resources/demo-users/user/sample-components/ns2/ec-integration-test.yaml
```

Alternatively, you can provide the content from that YAML using the UI:

1. :gear: Login as user2 and navigate to your application and component.

2. :gear: Click the `Integration tests` tab.

3. :gear: Click `Actions` and select `Add Integration test`.

4. :gear: Fill-in the details from the YAML.

5. :gear: Click `Add Integration test`.

Either way, you should now see the test listed in the UI under `Integration tests`.

Our integration test is using a pipeline residing in the location defined under the
`resolverRef` field on the YAML mentioned above. From now on, after the build pipeline
runs, the pipeline mentioned on the integration test will also be triggered.

:gear: To verify that, go back to your GitHub PR and add a comment: `/retest`.

On the Konflux UI, under your component `Activity` tab, you should now see the build
pipeline running again (`test-component-on-pull-request-...`), and when it's done, you
should see another pipeline run called `test-component-c6glg-...` being triggered.

You can click it and examine the logs to see the kind of things it verifies, and confirm
it passes successfully.

#### Add Customized Integration Tests (Optional)

**NOTE:** The custom integration test currently only supports testing images stored
externally to the cluster. If using the local registry, skip to
[Configure Releases](#configure-releases).

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

1. :gear: Login as user2 and navigate to your application and component.

2. :gear: Click the `Integration tests` tab.

3. :gear: Click `Actions` and select `Add Integration test`.

4. :gear: Fill in the fields:

* Integration test name: a name of your choice
* GitHub URL: `https://github.com/konflux-ci/testrepo`
* Revision: `main`
* Path in repository: `integration-tests/testrepo-integration.yaml`

5. :gear: Click `Add Integration test`.

Alternatively, you can create it using `kubectl`. The manifest is stored in
`test/resources/demo-users/user/sample-components/ns2/integration-test-hello.yaml`:

1. :gear: Verify the `application` field contains your application name.

2. :gear: Deploy the manifest:

```bash
kubectl create -f ./test/resources/demo-users/user/sample-components/ns2/integration-test-hello.yaml
```

:gear: Post a `/retest` comment on your GitHub PR, and once the `pull-request`
pipeline is done, you should see your new integration test being triggered alongside
the one you had before.

If you examine the logs, you should be able to see the snapshot being parsed and the
test being executed.

### Configure Releases

You will now configure Konflux to release your application to the registry.

This requires:

* A pipeline that will run on push events to the component repository.

* `ReleasePlan` and `ReleasePlanAdmission` resources, that will react on the snapshot to
  be created after the on-push pipeline will be triggered, which, in turn, will trigger
  the creation of the release.

If onboarded using the Konflux UI, the pipeline was already created and configured for
you.

If onboarded using Kubernetes manifests then you should have copied the pipeline to the
`.tekton` directory before [creating your initial PR](#creating-a-pull-request).

#### Create ReleasePlan and ReleasePlanAdmission Resources

Once you merge a PR, the on-push pipeline will be triggered and once it completes, a
snapshot will be created and the integration tests will run against the container images
built on the on-push pipeline.

Konflux now needs `ReleasePlan` and `ReleasePlanAdmission` resources that will be used
together with the snapshot for creating a new `Release` resource.

The `ReleasePlan` resource includes a reference to the application that the development
team wants to release, along with the namespace where the application is supposed to be
released (in this case, `managed-ns2`).

The `ReleasePlanAdmission` resource defines how the application should be released, and
it is typically maintained, not by the development team, but by the managed environment
team (the team that supports the deployments of that application).

The `ReleasePlanAdmission` resource makes use of an Enterprise Contract (EC) policy,
which defines criteria for gating releases.

For more details you can examine the manifests under the
[managed-ns2 directory](../test/resources/demo-users/user/sample-components/managed-ns2/).

To do all that, follow these steps:

:gear: Edit the `ReleasePlan` manifest at
[test/resources/demo-users/user/sample-components/ns2/release-plan.yaml](../test/resources/demo-users/user/sample-components/ns2/release-plan.yaml)
and verify that the `application` field contains the name of your application.

:gear: Deploy the Release Plan under the development team namespace (`user-ns2`):

```bash
kubectl create -f ./test/resources/demo-users/user/sample-components/ns2/release-plan.yaml
```

Edit the `ReleasePlanAdmission` manifest at
[test/resources/demo-users/user/sample-components/managed-ns2/rpa.yaml](../test/resources/demo-users/user/sample-components/managed-ns2/rpa.yaml):

**NOTE:** if you're using the in-cluster registry, you should not be required to make
any of the changes to the `ReleasePlanAdmission` manifest described below before
deploying it.

1. :gear: Under `applications`, verify that your application is the one listed.

2. :gear: Under the components mapping list, set the `name` field so it matches the name
   of your component and replace the value of the `repository` field with the URL of the
   repository on the registry to which your released images are to be pushed. This is
   typically a different repository comparing to the one builds are being pushed during
   tests.

   For example, if your component is called `test-component`, and you wish to release
   your images to a Quay.io repository called `my-user/my-konflux-component-release`,
   then the configs should look like this:

```yaml
    mapping:
      components:
        - name: test-component
          repository: quay.io/my-user/my-konflux-component-release
```

3. :gear: The example release pipeline requires a repository into which trusted
   artifacts will be written as a manner of passing data between tasks in the pipeline.

   The **ociStorage** field tells the pipeline where to have that stored.

   For example, if your release repository was
   `quay.io/my-user/my-konflux-component-release`, you could set your TA repository
   like this:

```yaml
    ociStorage: registry-service.kind-registry/test-component-release-ta
```

   For more details, see
   [Trusted Artifacts (ociStorage)](./registry-configuration.md#configuring-a-push-secret-for-the-release-pipeline).

Deploy the managed environment team's namespace:

```bash
kubectl apply -k ./test/resources/demo-users/user/sample-components/managed-ns2
```

At this point, you can click **Releases** on the left pane in the UI. The status
for your ReleasePlan should be **"Matched"**.

#### Create a Registry Secret for the Managed Namespace

**NOTE:** if you're using the in-cluster registry, you can skip this step and proceed
to [triggering a release](#trigger-the-release).

In order for the release service to be able to push images to the registry, a secret is
needed on the managed namespace (`managed-ns2`).

The secret needs to be created on this namespace regardless of whether you used the
UI for onboarding or not, but if you weren't, then this secret is identical to the one
that was previously created on the development namespace (`user-ns2`).

:gear: To create it, follow the instructions for
[creating a push secret for the release pipeline](./registry-configuration.md#configuring-a-push-secret-for-the-release-pipeline)
for namespace `managed-ns2`.

#### Trigger the Release

You can now merge your PR and observe the behavior:

1. Merge the PR in GitHub.

2. On the Konflux UI, you should now see your on-push pipeline being triggered.

3. Once it finishes successfully, the integration tests should run once more, and
   a release should be created under the `Releases` tab.

4. :gear: Wait for the Release to be complete, and check your registry repository for
   the released image.

**Congratulations**: You just created a release for your application!

Your released image should be available inside the repository pointed by your
`ReleasePlanAdmission` resource.

### Working with External Image Registry (Optional)

This section provides instructions if you're interested in using an external image
registry, instead of the in-cluster one.

#### Push Pull Request Builds to External Registry

First, configure your application to use an external registry instead of the internal
one used so far. In order to do that, you'd need to have a repository, on a public
registry, in which you have push permissions.
E.g. [Docker Hub](https://hub.docker.com/), [Quay.io](https://quay.io/repository/):

1. :gear: Create an account on a public registry (unless you have one already).

2. :gear: Create a
   [push secret](./registry-configuration.md#configuring-a-push-secret-for-the-build-pipeline)
   based on your login information and deploy it to your user namespace on the cluster
   (e.g. `user-ns2`).

3. :gear: Create a new repository on the registry to which your images will be pushed.
   For example, in Quay.io, you'd need to click the
   [Create New Repository](https://quay.io/new/) button and provide it with name and
   location. Free accounts tend to have limits on private repositories, so for the
   purpose of this example, you can make your repository public.

4. Configure your build pipeline to use your new repository on the public registry
   instead of the local registry:

   :gear: Edit `.tekton/testrepo-pull-request.yaml` inside your `testrepo` fork
   and replace the value of `output-image` to point to your repository. For example,
   if using Quay.io and your username is `my-user` and you created a repository called
   `my-konflux-component` under your own organization, then the configs should look like this:

```yaml
  - name: output-image
    value: quay.io/my-user/my-konflux-component:on-pr-{{revision}}
```

5. :gear: Push your changes to your `testrepo` fork, either as a new PR or as a change
   to an existing PR. Observe the behavior as before, and verify that the build
   pipeline finishes successfully, and that your public repository contains the images
   pushed by the pipeline.

#### Use External Registry for on-push Pipeline

:gear: Edit the content of the copy you made earlier to the on-push pipeline at
`.tekton/testrepo-push.yaml`, replacing the value of `output-image`, so that the
repository URL is identical to the one
[previously set](#push-pull-request-builds-to-external-registry)
for the `pull-request` pipeline.

For example, if using Quay.io and your username is `my-user` and you created a
repository called `my-konflux-component` under your own organization, then the configs
should look like this:

```yaml
  - name: output-image
    value: quay.io/my-user/my-konflux-component:{{revision}}
```

**Note:** this is the same as for the pull request pipeline, but the tag portion now
only includes the revision.

