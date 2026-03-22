---
title: "Configure Releases"
linkTitle: "Releases"
weight: 3
description: "Configure ReleasePlan and ReleasePlanAdmission resources to release your application to a container registry."
---

You will now configure Konflux to release your application to the registry.

This requires:

- A pipeline that will run on push events to the component repository.
- `ReleasePlan` and `ReleasePlanAdmission` resources that will react on the snapshot to
  be created after the on-push pipeline is triggered, which in turn will trigger the
  creation of the release.

If onboarded using the Konflux UI, the pipeline was already created and configured for
you.

If onboarded using Kubernetes manifests, you should have copied the pipeline to the
`.tekton` directory before [creating your initial PR]({{< relref "onboarding#creating-a-pull-request" >}}).

## Create ReleasePlan and ReleasePlanAdmission Resources

Once you merge a PR, the on-push pipeline will be triggered and once it completes, a
snapshot will be created and the integration tests will run against the container images
built on the on-push pipeline.

Konflux now needs `ReleasePlan` and `ReleasePlanAdmission` resources that will be used
together with the snapshot for creating a new `Release` resource.

The `ReleasePlan` resource includes a reference to the application that the development
team wants to release, along with the namespace where the application is supposed to be
released (in this case, `managed-ns2`).

The `ReleasePlanAdmission` resource defines how the application should be released, and
it is typically maintained not by the development team, but by the managed environment
team (the team that supports the deployments of that application).

The `ReleasePlanAdmission` resource makes use of an Enterprise Contract (EC) policy,
which defines criteria for gating releases.

For more details you can examine the manifests under the
`test/resources/demo-users/user/sample-components/managed-ns2/` directory.

To do all that, follow these steps:

1. Edit the `ReleasePlan` manifest at
   `test/resources/demo-users/user/sample-components/ns2/release-plan.yaml`
   and verify that the `application` field contains the name of your application.

2. Deploy the Release Plan under the development team namespace (`user-ns2`):

   ```bash
   kubectl create -f ./test/resources/demo-users/user/sample-components/ns2/release-plan.yaml
   ```

3. Edit the `ReleasePlanAdmission` manifest at
   `test/resources/demo-users/user/sample-components/managed-ns2/rpa.yaml`.

   {{< alert color="info" >}}
   If you're using the in-cluster registry, you are not required to make any of the
   changes to the <code>ReleasePlanAdmission</code> manifest described below before deploying it.
   {{< /alert >}}

   - Under `applications`, verify that your application is the one listed.

   - Under the components mapping list, set the `name` field so it matches the name
     of your component and replace the value of the `repository` field with the URL of
     the repository on the registry to which your released images are to be pushed. This
     is typically a different repository from the one builds are pushed to during tests.

     For example, if your component is called `test-component` and you wish to release
     your images to a Quay.io repository called `my-user/my-konflux-component-release`:

     ```yaml
         mapping:
           components:
             - name: test-component
               repository: quay.io/my-user/my-konflux-component-release
     ```

   - The example release pipeline requires a repository into which trusted artifacts
     will be written as a manner of passing data between tasks in the pipeline.

     The **ociStorage** field tells the pipeline where to have that stored. For example:

     ```yaml
         ociStorage: registry-service.kind-registry/test-component-release-ta
     ```

4. Deploy the managed environment team's namespace:

   ```bash
   kubectl apply -k ./test/resources/demo-users/user/sample-components/managed-ns2
   ```

At this point, you can click **Releases** on the left pane in the UI. The status
for your ReleasePlan should be **"Matched"**.

## Create a Registry Secret for the Managed Namespace

{{< alert color="info" >}}
If you're using the in-cluster registry, you can skip this step and proceed to
<a href="#trigger-the-release">Trigger the Release</a>.
{{< /alert >}}

In order for the release service to be able to push images to the registry, a secret is
needed on the managed namespace (`managed-ns2`).

The secret needs to be created on this namespace regardless of whether you used the
UI for onboarding or not, but if you weren't, then this secret is identical to the one
that was previously created on the development namespace (`user-ns2`).

To create it, follow the instructions for [creating a push secret for the release
pipeline]({{< relref "../guides/registry-configuration#release-pipeline-push-secret" >}}) for namespace `managed-ns2`.

## Trigger the Release

You can now merge your PR and observe the behavior:

1. Merge the PR in GitHub.
2. On the Konflux UI, you should now see your on-push pipeline being triggered.
3. Once it finishes successfully, the integration tests should run once more, and
   a release should be created under the `Releases` tab.
4. Wait for the Release to be complete, and check your registry repository for
   the released image.

**Congratulations!** You just created a release for your application.

Your released image should be available inside the repository pointed by your
`ReleasePlanAdmission` resource.

## Working with External Image Registry (Optional)

This section provides instructions if you're interested in using an external image
registry instead of the in-cluster one.

### Push Pull Request Builds to External Registry

First, configure your application to use an external registry instead of the internal
one. To do this, you need a repository on a public registry where you have push
permissions (e.g. [Docker Hub](https://hub.docker.com/), [Quay.io](https://quay.io/repository/)):

1. Create an account on a public registry (unless you have one already).

2. Create a push secret based on your login information and deploy it to your user
   namespace on the cluster (e.g. `user-ns2`).

3. Create a new repository on the registry to which your images will be pushed.
   For example, in Quay.io, click the
   [Create New Repository](https://quay.io/new/) button and provide it with a name and
   location. Free accounts tend to have limits on private repositories, so for the
   purpose of this example, you can make your repository public.

4. Configure your build pipeline to use your new repository on the public registry
   instead of the local registry.

   Edit `.tekton/testrepo-pull-request.yaml` inside your `testrepo` fork and replace
   the value of `output-image` to point to your repository. For example, if using
   Quay.io with username `my-user` and a repository called `my-konflux-component`:

   ```yaml
     - name: output-image
       value: quay.io/my-user/my-konflux-component:on-pr-{{revision}}
   ```

5. Push your changes to your `testrepo` fork, either as a new PR or as a change
   to an existing PR. Observe the behavior as before, and verify that the build
   pipeline finishes successfully, and that your public repository contains the images
   pushed by the pipeline.

### Use External Registry for on-push Pipeline

Edit the content of the copy you made earlier to the on-push pipeline at
`.tekton/testrepo-push.yaml`, replacing the value of `output-image` so that the
repository URL is identical to the one
[previously set](#push-pull-request-builds-to-external-registry)
for the `pull-request` pipeline.

For example, if using Quay.io with username `my-user` and a repository called
`my-konflux-component`:

```yaml
  - name: output-image
    value: quay.io/my-user/my-konflux-component:{{revision}}
```

{{< alert color="info" >}}
This is the same as for the pull request pipeline, but the tag portion now only
includes the revision (no <code>on-pr-</code> prefix).
{{< /alert >}}
