---
title: "Conforma Policy Configuration"
linkTitle: "Conforma Policy Configuration"
weight: 7
description: "How to use and customize Conforma policies for integration tests and release."
---

[Conforma](https://conforma.dev/docs/user-guide/index.html) (previously known as
Enterprise Contract) is the policy verification tool integrated into Konflux. It
defines which checks are performed on your container images before they can be
released. Conforma policies are evaluated in two places:

- **Integration tests** - after every build pipeline run, before a snapshot is
  promoted.
- **Release** - as a gating step in the release pipeline, referenced from a
  `ReleasePlanAdmission`.

For a broader overview of where policies are evaluated in your workflow, see
[Policy Evaluations](https://konflux-ci.dev/docs/compliance/policy-evaluations/)
in the Konflux documentation.

## Pre-deployed policies

The operator deploys six `EnterpriseContractPolicy` CRs into the
`enterprise-contract-service` namespace. These are publicly readable by any
authenticated cluster user and can be referenced directly without creating your own.

| CR name | Display name | Rule collection(s) | Description |
|---------|-------------|--------------------|-------------|
| `default` | Default | `@slsa3` | Used for new Konflux applications. Covers SLSA levels 1–3. |
| `slsa3` | SLSA3 | `@minimal` + `@slsa3` | SLSA levels 1–3 plus basic Konflux checks. |
| `redhat` | Red Hat | `@redhat` | Full set of rules required internally by Red Hat when building Red Hat products. |
| `redhat-no-hermetic` | Red Hat (non hermetic) | `@redhat` (excludes `hermetic_build_task` and `prefetch-dependencies`) | Red Hat rules for builds that do not use hermetic mode. |
| `redhat-trusted-tasks` | Red Hat Trusted Tasks | `kind` | Validates **Tekton Task definitions** against Red Hat standards. Uses the `task-policy` bundle. |
| `all` | Everything (experimental) | `*` | Every available rule. **Not expected to pass** without exclusions - for exploration only. |

List the policies on a running cluster:

```bash
kubectl get enterprisecontractpolicy -n enterprise-contract-service
```

Inspect any individual policy:

```bash
kubectl get enterprisecontractpolicy default -n enterprise-contract-service -o yaml
```

## Using a policy in integration tests

When you create an application through the Konflux UI, an `IntegrationTestScenario`
that runs the Conforma pipeline is created automatically. It references the
[enterprise-contract pipeline](https://github.com/konflux-ci/build-definitions/blob/main/pipelines/enterprise-contract.yaml)
from the `konflux-ci/build-definitions` repository and uses the `default` policy.

You can inspect the integration test scenarios in your namespace:

```bash
kubectl get integrationtestscenario -n <your-namespace>
kubectl get integrationtestscenario <test-name> -n <your-namespace> -o yaml
```

To switch to a different policy, set the `POLICY_CONFIGURATION` parameter on the
`IntegrationTestScenario`. The value can be:

- A **namespace-qualified CR name**: `<namespace>/<cr-name>` (e.g. a pre-deployed
  policy or one you have created in your own namespace).
- A **git URL** pointing to a policy configuration file (e.g.
  `github.com/conforma/config//slsa3`).

### Via the Konflux UI

1. Open your application and go to the **Integration tests** tab.
2. Select the three dots next to the Enterprise Contract test and choose **Edit**.
3. Click **Add parameter**.
4. Set **Name** to `POLICY_CONFIGURATION`.
5. Set **Value** to the policy reference, for example
   `enterprise-contract-service/redhat-no-hermetic`.
6. Click **Save changes**.

For the full procedure, see
[Configuring the enterprise contract policy](https://konflux-ci.dev/docs/testing/integration/editing/#configuring-the-enterprise-contract-policy)
in the Konflux documentation.

### Via the CLI

Edit the `IntegrationTestScenario` directly:

```bash
kubectl edit integrationtestscenario <test-name> -n <your-namespace>
```

Add or update the `params` key under `spec`:

```yaml
spec:
  application: my-application
  params:
    - name: POLICY_CONFIGURATION
      value: enterprise-contract-service/redhat-no-hermetic
```

To trigger a new integration test run after saving, open a pull request or comment
`/retest` on an existing one.

## Using a policy in release

The release pipeline validates the snapshot against a Conforma policy before
proceeding. The policy is configured in the `spec.policy` field of the
`ReleasePlanAdmission` CR that lives in the managed tenant namespace.

```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: ReleasePlanAdmission
metadata:
  name: sre-production
  namespace: managed-tenant-namespace
spec:
  applications:
    - my-application
  origin: <dev-tenant-namespace>
  pipeline:
    pipelineRef: <pipeline-ref>
    serviceAccountName: release-pipeline
  policy: default  # (1)
```

**(1)** The `policy` field is **required** and accepts a bare policy name — the name
of an `EnterpriseContractPolicy` CR in the `enterprise-contract-service` namespace
(e.g. `default`, `redhat-no-hermetic`). It must match the pattern
`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`

For complete details on creating and configuring a `ReleasePlanAdmission`, see
[Creating a release plan admission](https://konflux-ci.dev/docs/releasing/create-release-plan-admission/)
in the Konflux documentation.

## Creating a custom policy

If none of the pre-deployed policies suit your use case, you can define your own.
There are two approaches:

### Option A: Git URL

Point directly to a policy configuration file in a git repository. Several
[predefined configurations](https://github.com/conforma/config) are available in the
`conforma/config` repository. For example, to use the SLSA level 3 configuration
hosted there:

```
github.com/conforma/config//slsa3
```

The `//` syntax separates the git repository URL from the subdirectory path. Conforma
looks for a `policy.yaml` or `.ec/policy.yaml` file in the specified directory.

Use this value directly as the `POLICY_CONFIGURATION` parameter or `spec.policy`
field - no cluster resource needs to be created.

### Option B: EnterpriseContractPolicy CR

Create an `EnterpriseContractPolicy` CR in your namespace for full control over which
rules are included or excluded. You can use any of the pre-deployed policies as a
starting point.

Create a file named `policy.yaml` and adjust it to your requirements:

```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: EnterpriseContractPolicy
metadata:
  name: my-custom-policy
  namespace: <your-namespace>
spec:
  description: A custom Conforma policy for my application
  publicKey: k8s://openshift-pipelines/public-key
  sources:
    - name: Release policies
      policy:
        - oci::quay.io/conforma/release-policy:konflux
      data:
        - oci::quay.io/konflux-ci/tekton-catalog/data-acceptable-bundles:latest
        - github.com/release-engineering/rhtap-ec-policy//data
      config:
        include:
          - "@slsa3"
        exclude:
          - hermetic_build_task.*
```

Apply it to the cluster:

```bash
kubectl apply -f policy.yaml
```

**Integration tests** — reference it as `<your-namespace>/my-custom-policy` in the
`POLICY_CONFIGURATION` parameter of your `IntegrationTestScenario`.

**Release** — the `ReleasePlanAdmission.spec.policy` field only accepts a bare policy
name and the release service looks up policies in the `enterprise-contract-service`
namespace. To use a custom policy for release, deploy it there instead:

```bash
kubectl apply -f policy.yaml -n enterprise-contract-service
```

Then set `policy: my-custom-policy` in your `ReleasePlanAdmission`.

See the [Conforma configuration reference](https://conforma.dev/docs/cli/configuration.html)
for the full set of available `include`/`exclude` options and rule collections.

## Customizing an existing policy to waive violations

If Conforma reports a violation that you cannot remedy by changing the build process,
you can waive the failing check by customizing the policy. The recommended workflow is:

1. [Identify](https://konflux-ci.dev/docs/compliance/policy-evaluations/) which policy
   is being used for your application.
2. Decide whether to modify the shared policy or create a new one. Creating a new
   policy scoped to your namespace avoids impacting other users.
3. Copy the relevant pre-deployed policy as a starting point, add your exclusions, and
   apply it to your namespace (see [EnterpriseContractPolicy CR](#option-b-enterprisecontractpolicy-cr)
   above).
4. Update your [integration test](#using-a-policy-in-integration-tests) and
   [ReleasePlanAdmission](#using-a-policy-in-release) to reference the new policy.

See [Customizing Policy](https://konflux-ci.dev/docs/compliance/customizing-policy/)
in the Konflux documentation for more details.
