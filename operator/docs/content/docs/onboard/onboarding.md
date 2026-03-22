---
title: "Onboard a new Application"
linkTitle: "Onboarding"
weight: 1
description: "Onboard a sample application to Konflux using the UI or Kubernetes manifests, observe a build, and pull the resulting image."
---

{{< alert color="info" >}}
All commands shown in this document assume you are in the repository root.
{{< /alert >}}

## Onboard a new Application

Onboard an application to Konflux on behalf of user2.

This section includes two options for onboarding an application to Konflux.

The first option uses the Konflux UI to onboard an application and release
its builds to Quay.io.

The second option uses Kubernetes manifests to onboard and releases builds to a
container registry deployed to the cluster. This approach simplifies onboarding
to demonstrate Konflux more easily.

Both options will use an example repository containing a Dockerfile to be built by
Konflux:

1. Fork the [example repository](https://github.com/konflux-ci/testrepo), by
   clicking the `Fork` button from that repository and following the instructions on the
   "Create a new fork" page.

2. Install the GitHub app on your fork: Go to the app's page on GitHub, click on
   Install App on the left-hand side, Select the organization the fork repository is on,
   click `Only select repositories`, and select your fork repository.

We will use our Konflux deployment to build and release Pull Requests for this fork.

### Option 1: Onboard Application with the Konflux UI

With this approach, Konflux can create:

1. The manifests in GitHub for the pipelines it will run against the applications
   onboarded to Konflux.
2. The Quay.io repositories into which it will push container images.

#### Create a GitHub Application

Pipeline creation requires the [GitHub Application Secrets]({{< relref "../guides/github-secrets" >}}) **on all 3 namespaces** and
installing your newly-created GitHub app on your repository, as explained above.

#### Configure Quay.io

Create an organization and an application in Quay.io that will allow Konflux to
create repositories for your applications. To do that, follow the procedure to
[configure a Quay.io application and deploy `image-controller`]({{< relref "../guides/registry-configuration#quayio-auto-provisioning-image-controller" >}}).

#### Create Application and Component via the Konflux UI

Follow these steps to onboard your application:

1. Login to [Konflux](https://localhost:9443) as `user2@konflux.dev` (password: `password`).
2. Click `Create application`.
3. Verify the workspace is set to `user-ns2`.
4. Provide a name to the application and click "Add a component".
5. Under `Git repository url`, copy the **https** link to your fork. This should
   be something similar to `https://github.com/<your-name>/testrepo.git`.
6. Uncheck the "Should the image produced be private" checkbox. If left checked, Pull Request
   pipelines will not start because image-controller will fail to provision the image
   repository.
7. Leave `Docker file` blank. The default value of `Dockerfile` will be used.
8. Under the Pipeline drop-down list, select `docker-build-oci-ta`.
9. Click `Create application`.

{{< alert color="warning" >}}
If you encounter a <code>404 Not Found</code> error, refer to the <a href="{{< relref "../troubleshooting#unable-to-create-application-with-component-using-the-konflux-ui" >}}">troubleshooting guide</a>.
{{< /alert >}}

The UI should now display the Lifecycle diagram for your application. In the Components
tab you should be able to see your component listed and you'll be prompted to merge the
automatically-created Pull Request (don't do that just yet — we'll have it merged in
the [Trigger the Release]({{< relref "release#trigger-the-release" >}}) section).

{{< alert color="info" >}}
If you have <strong>not</strong> completed the <a href="#configure-quayio">Quay.io setup steps</a> in the previous section,
Konflux will be unable to send a PR to your repository. Konflux will display "Sending
Pull Request".
{{< /alert >}}

In your GitHub repository you should now see a PR was created with two new pipelines.
One is triggered by PR events (e.g. when PRs are created or changed), and the other is
triggered by push events (e.g. when PRs are merged).

Your application is now onboarded, and you can continue to the
[Observe the Behavior](#observe-the-behavior) section.

### Option 2: Onboard Application with Kubernetes Manifests

With this approach, we use `kubectl` to deploy the manifests for creating the
`Application` and `Component` resources and we manually create the PR for introducing
the pipelines to run using Konflux.

To do that:

1. Use a text editor to edit your local copy of
   `test/resources/demo-users/user/sample-components/ns2/application-and-component.yaml`.

   Under the `Component` and `Repository` resources, change the `url` fields so they
   point to your newly-created fork.

   Note the format differences between the two fields — the `Component` URL has a `.git`
   suffix, while the `Repository` URL does not.

   Deploy the manifests:

   ```bash
   kubectl create -f ./test/resources/demo-users/user/sample-components/ns2/application-and-component.yaml
   ```

2. Log into the Konflux UI as `user2@konflux.dev` (password: `password`). You should be
   able to see your new Application and Component by clicking "View my applications".

#### Image Registry

The build pipeline that you're about to run pushes the images it builds to an image
registry.

For the sake of simplicity, it's configured to use a registry deployed into the
cluster during previous steps of this setup (when dependencies were installed).

{{< alert color="info" >}}
The statement above is only true when not onboarding via the Konflux UI.
You can convert it to use a public image registry later on.
{{< /alert >}}

#### Creating a Pull Request

You're now ready to create your first PR **to your fork**.

1. Clone your fork and create a new branch:

   ```bash
   git clone <my-fork-url>
   cd <my-fork-name>
   git checkout -b add-pipelines
   ```

2. Tekton will trigger pipelines present in the `.tekton` directory. The pipelines
   already exist on your repository; you just need to copy them to that location.

   Copy the manifests:

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

4. Your terminal should now display a link for creating a new Pull Request in
   GitHub. Click the link, **make sure the PR is targeted against your fork's `main`
   branch and not against the repository from which it was forked** (i.e.
   `base repository` should reside under your user name).

   Finally, click "Create pull request" (we'll have it merged in the
   [Trigger the Release]({{< relref "release#trigger-the-release" >}}) section).

## Observe the Behavior

{{< alert color="info" >}}
If the behavior you see is not as described below, consult the <a href="{{< relref "../troubleshooting#pipelines-not-triggering-on-prs" >}}">troubleshooting document</a>
for Pipelines not triggering on PRs.
{{< /alert >}}

Once your PR is created, you should see a status being reported at the bottom of the
PR's comments section (just above the "Add a comment" box).

Your GitHub App should now send PR events to your smee channel. Navigate to your smee
channel's web page. You should see a couple of events were sent just after your PR was
created (e.g. `check_run`, `pull_request`).

Log into the Konflux UI as user2 and check your applications. Select the
application you created earlier, click on `Activity` and `Pipeline runs`. A build
should've been triggered a few seconds after the PR was created.

Follow the build progress. Depending on your system's load and network connection (the
build process involves pulling images), it might take a few minutes for the build to
complete. It will clone the repository, build using the Dockerfile, and push the image
to the registry.

{{< alert color="warning" >}}
If a pipeline is triggered but seems stuck for a long time, especially at early stages,
refer to the <a href="{{< relref "../troubleshooting#running-out-of-resources" >}}">Running out of resources</a> troubleshooting section.
{{< /alert >}}

## Pull your new Image

When the build process is done, you can check out the image you just built by pulling it
from the registry.

### Public Registry

If using a public registry, navigate to the repository URL mentioned in the
`output-image` value of your pull-request pipeline and locate your build.

For example, if using [Quay.io](https://quay.io/repository/), go to the
`Tags` tab and locate the relevant build for the tag mentioned on the `output-image`
value (e.g. `on-pr-{{revision}}`), and click the `Fetch Tag` button on the right to
generate the command to pull the image.

### Local Registry

If using a local registry, port-forward the registry service so you can reach it
from outside of the cluster:

```bash
kubectl port-forward -n kind-registry svc/registry-service 30001:443
```

The local registry is using a self-signed certificate that is being distributed to all
namespaces. You can fetch the certificate from the cluster and use it on the `curl`
calls below:

```bash
kubectl get secrets -n kind-registry local-registry-tls \
   -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt

curl --cacert ca.crt https://...
```

Alternatively, use the `-k` flag to skip TLS verification.

Leave the terminal hanging and on a new terminal window, list the repositories on the
registry:

```bash
curl -k https://localhost:30001/v2/_catalog
```

The output should look like this:

```bash
{"repositories":["test-component"]}
```

List the tags on the `test-component` repository (assuming you did not change the
pipeline's `output-image` parameter):

```bash
curl -k https://localhost:30001/v2/test-component/tags/list
```

You should see a list of tags pushed to that repository:

```bash
{"name":"test-component","tags":["on-pr-1ab9e6d756fbe84aa727fc8bb27c7362d40eb3a4","sha256-b63f3d381f8bb2789f2080716d88ed71fe5060421277746d450fbcf938538119.sbom"]}
```

Pull the image starting with `on-pr-` (using `podman` below, but the commands should be
similar for `docker`):

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

### Start a Container

Start a container based on the image you pulled:

```bash
podman run --rm be9a47b76264e8fb324d9ef7cddc9...
hello world
```

## What's Next?

Now that your application is onboarded and you've verified the build pipeline works,
configure integration tests to automatically validate your builds:

[Integration Tests →]({{< relref "integration" >}})
