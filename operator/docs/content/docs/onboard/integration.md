---
title: "Integration Tests"
linkTitle: "Integration Tests"
weight: 2
description: "Configure and run integration tests for your application after each build pipeline completes."
---

If you onboarded your application using the Konflux UI, the integration tests
are automatically created for you by Konflux.

On the Konflux UI, the integration tests definition should be visible in the
`Integration tests` tab under your application, and a pipeline should've been triggered
for them under the `Activity` tab, named after the name of the application. You can
click it and examine the logs to see the kind of things it verifies, and to confirm it
passed successfully.

Once confirmed, skip to [Add Customized Integration Tests](#add-customized-integration-tests-optional).

If you onboarded your application manually, you will now configure your application to
trigger integration tests after each PR build is done.

## Configure Integration Tests

You can add integration tests either via the Konflux UI, or by applying the equivalent
Kubernetes resource.

{{< alert color="info" >}}
If you imported your component via the UI, a similar Integration Test is pre-installed.
{{< /alert >}}

In our case, the resource is defined in
`test/resources/demo-users/user/sample-components/ns2/ec-integration-test.yaml`.

Apply the resource manifest:

```bash
kubectl create -f ./test/resources/demo-users/user/sample-components/ns2/ec-integration-test.yaml
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

## Add Customized Integration Tests (Optional)

{{< alert color="warning" >}}
The custom integration test currently only supports testing images stored externally to
the cluster. If using the local registry, skip to
<a href="{{< relref "release" >}}">Configure Releases</a>.
{{< /alert >}}

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

##### Using the Konflux UI

1. Login as user2 and navigate to your application and component.
2. Click the `Integration tests` tab.
3. Click `Actions` and select `Add Integration test`.
4. Fill in the fields:
   - **Integration test name:** a name of your choice
   - **GitHub URL:** `https://github.com/konflux-ci/testrepo`
   - **Revision:** `main`
   - **Path in repository:** `integration-tests/testrepo-integration.yaml`
5. Click `Add Integration test`.

##### Using kubectl

The manifest is stored in
`test/resources/demo-users/user/sample-components/ns2/integration-test-hello.yaml`:

1. Verify the `application` field contains your application name.
2. Deploy the manifest:

   ```bash
   kubectl create -f ./test/resources/demo-users/user/sample-components/ns2/integration-test-hello.yaml
   ```

Post a `/retest` comment on your GitHub PR, and once the `pull-request` pipeline is
done, you should see your new integration test being triggered alongside the one you had
before.

If you examine the logs, you should be able to see the snapshot being parsed and the
test being executed.

## What's Next?

With integration tests in place, you're ready to configure the release pipeline and
publish your application to a registry:

[Configure Releases →]({{< relref "release" >}})
