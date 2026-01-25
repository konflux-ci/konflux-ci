# Using Konflux

This guide demonstrates building, testing, and releasing applications with Konflux.

For comprehensive documentation including advanced workflows and API references, see the [Konflux Documentation](https://konflux-ci.dev/docs/).

## Prerequisites

Before onboarding an application, ensure you have:
- A namespace with the `konflux-ci.dev/type=tenant` label
- User access granted through RBAC (see [Namespace and User Management](operator-deployment.md#namespace-and-user-management))
- GitHub App installed on your repository
- Demo users deployed (for local testing) or proper authentication configured (for production)

Fork the [example repository](https://github.com/konflux-ci/testrepo) to have a working application for testing.

## Using the Konflux UI

The UI approach provides a streamlined onboarding experience. Note that the UI does not automatically set the `containerImage` field on Components. You have two options:

**Option 1: Automatic repository provisioning (recommended)** - Configure the image-controller to automatically create Quay.io repositories and set the `containerImage` field. Follow the [automatic repository provisioning procedure](./registry-configuration.md#automatic-repository-provisioning-quayio) to set this up.

**Option 2: Manual configuration** - After creating the Component through the UI, manually edit it to add the `containerImage` field pointing to your chosen registry. See the [registry configuration guide](./registry-configuration.md) for options.

Log into https://localhost:9443 with demo credentials. Click "Create application" and verify the workspace shows your namespace. Provide an application name and click "Add a component."

Enter the HTTPS URL to your fork under "Git repository url." Leave "Docker file" blank to use the default Dockerfile. Select `docker-build-oci-ta` from the Pipeline dropdown.

Click "Create application" to complete the onboarding. Konflux creates a pull request in your repository with pipeline definitions. The Components tab displays your component and prompts you to merge this PR.

## Using Kubernetes Manifests

The manifest approach works with any image registry and gives you direct control over the configuration.

**Important:** When creating Components via manifests, you must set the `containerImage` field to specify where built images should be stored. See the [registry configuration guide](./registry-configuration.md) for options including the internal registry (for local development), external registries (Quay.io, Docker Hub, etc.), or automatic repository provisioning.

Edit your local copy of [example application manifests](../test/resources/demo-users/user/ns2/application-and-component.yaml). Update the `url` fields under Component and Repository resources to point to your fork. Note that the Component URL includes a `.git` suffix while the Repository URL does not.

Deploy the manifests:

```bash
kubectl create -f ./test/resources/demo-users/user/ns2/application-and-component.yaml
```

The example manifests use the internal registry deployed with Konflux. For external registries, see [Working with External Image Registries](#working-with-external-image-registries).

## Creating a Pull Request

Clone your fork and create a branch:

```bash
git clone <my-fork-url>
cd <my-fork-name>
git checkout -b add-pipelines
```

Copy pipeline definitions to the `.tekton` directory:

```bash
mkdir -p .tekton
cp pipelines/* .tekton/
```

Commit and push:

```bash
git add .tekton
git commit -m "add pipelines"
git push origin HEAD
```

Create the pull request on GitHub. Verify the PR targets your fork's main branch, not the upstream repository.

## Observing Pipeline Execution

After creating the PR, check the bottom of the PR page for pipeline status. Your smee channel receives PR events visible on its web page.

Log into the Konflux UI and navigate to Activity → Pipeline runs under your application. A build should trigger within seconds of PR creation.

The build clones the repository, builds the container image using your Dockerfile, and pushes the image to the registry. Build time depends on system resources and network speed.

If pipelines fail to trigger, see the [troubleshooting guide](./troubleshooting.md#pr-changes-are-not-triggering-pipelines).

## Integration Tests

Integration tests verify the built container images before release. The UI automatically creates basic Enterprise Contract tests when you onboard through that path.

For manual onboarding, create an IntegrationTestScenario resource:

```bash
kubectl create -f test/resources/demo-users/user/ns2/ec-integration-test.yaml
```

After the on-pull-request pipeline completes, Konflux triggers the integration test pipeline. Add a `/retest` comment to your PR to verify the integration test runs.

To add custom integration tests specific to your application, see the complete integration testing documentation in the [Konflux Documentation](https://konflux-ci.dev/docs/).

## Configuring Releases

Releases publish container images to a registry after integration tests pass. This requires ReleasePlan and ReleasePlanAdmission resources.

The ReleasePlan references your application and specifies the target namespace (typically a "managed" namespace representing a deployment environment). The ReleasePlanAdmission defines the release criteria and image destination.

Edit the [release plan](../test/resources/demo-users/user/ns2/release-plan.yaml) and verify the application name matches yours.

Deploy the ReleasePlan in your development namespace:

```bash
kubectl create -f ./test/resources/demo-users/user/ns2/release-plan.yaml
```

Edit the [ReleasePlanAdmission manifest](../test/resources/demo-users/user/managed-ns2/rpa.yaml). Update the component name and repository URL to match your configuration. The repository URL specifies where released images are pushed.

Deploy the managed namespace and ReleasePlanAdmission:

```bash
kubectl create -k ./test/resources/demo-users/user/managed-ns2
```

For external registries, create a push secret in the managed namespace following the [registry secret instructions](./registry-configuration.md#configuring-a-push-secret-for-the-release-pipeline).

Merge your PR to trigger the on-push pipeline. After the pipeline completes and integration tests pass, Konflux creates a Release. Check the Releases tab in the UI to verify completion, then find your released image in the configured registry repository.

## Working with External Image Registries

For comprehensive registry configuration including choosing the right registry, setting up authentication, and automatic repository provisioning, see the [registry configuration guide](./registry-configuration.md).

The internal registry works for local development but production deployments typically use external registries.

Create an account on a public registry like Docker Hub or Quay.io. Create a repository for your images.

Create a [push secret](./registry-configuration.md#configuring-a-push-secret-for-the-build-pipeline) in your namespace using your registry credentials.

Edit `.tekton/testrepo-pull-request.yaml` in your fork and update the `output-image` parameter to reference your registry repository:

```yaml
- name: output-image
  value: quay.io/my-user/my-component:on-pr-{{revision}}
```

Edit `.tekton/testrepo-push.yaml` similarly:

```yaml
- name: output-image
  value: quay.io/my-user/my-component:{{revision}}
```

Push these changes to your repository. Subsequent builds push images to your external registry.
