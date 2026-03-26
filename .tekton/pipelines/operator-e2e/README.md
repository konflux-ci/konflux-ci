# `operator-e2e-pipeline`

Tekton pipeline for operator E2E that provisions Kind on AWS, deploys Konflux, runs E2E tests, and deprovisions.

## Scope

- Operator install uses Tekton `none` mode: the deploy task runs out-of-cluster `bin/manager` (not an operator image).
- Deploy and tests are split across `deploy-konflux` and `konflux-e2e-tests`.
- There are **no Pipeline workspaces**: each Task clones `konflux-ci` into an `emptyDir` (see the Task YAML). This is so the pipeline can be triggered both by Pipelines as Code and as IntegrationTestScenario.

### Operator process vs. test phase

In `none` mode, `bin/manager` is started as a **background process inside the `deploy-konflux` Task pod** (see `scripts/operator-e2e/tekton-deploy-operator-and-wait.sh`). When that Task finishes, the pod exits and **that operator process terminates** — it is not left running on the Kind cluster.

The **`konflux-e2e-tests` Task** runs in a **separate pod** with kubeconfig only. There is **no `bin/manager` reconciliation loop** during integration or conformance tests. The cluster still runs the workloads the operator applied (build-service, integration-service, PaC, and so on); those controllers keep reconciling their own resources.

The current conformance suite is written for that model: it exercises deployed services and GitHub flows, not “delete a Deployment and expect the Konflux operator to recreate it.” A test that assumed operator-level reconciliation during the test phase would not behave like a long-running operator install.

## Inputs (params)

- `git-url` (default: `https://github.com/konflux-ci/konflux-ci.git`): repository URL used for clone + git-resolved local tasks.
- `revision` (required): git ref from `git-url` to test.
- `overrides-yaml` (default: empty): optional inline overrides consumed by deploy task.
- `konflux-ready-timeout` (default: `30m`): readiness timeout for Konflux CR.
- `oci-container-repo` (required): OCI registry/repo prefix for kind-aws provision/deprovision artifacts (logs/state); no tag suffix—the pipeline appends `:$(context.pipelineRun.name)` for provision/deprovision. The deploy task also pushes **post-prep** `operator/pkg/manifests` to the **same repo** with tag `$(context.pipelineRun.name).pkg-manifests` so it does not replace the provision artifact.
- `oci-container-repo-credentials-secret` (required): name of a Secret with registry credentials for `oci-container-repo` (kind-aws `oci-credentials`). This repo’s PAC PipelineRun uses `konflux-test-infra`.
- `aws-credentials-secret` (default: `konflux-mapt-us-east-1`): AWS credentials for provision task.
- `deprovision-aws-credentials-secret` (default: `konflux-mapt-us-east-1`): AWS credentials for deprovision task.
- `release-ta-oci-storage` (default: empty): optional OCI ref for conformance trusted-artifacts flow.
- `integration-go-test-extra-args` (default: empty): optional space-separated extra flags appended to integration `go test . ./pkg/...` (e.g. `-run=TestFoo -count=1`).
- `conformance-go-test-extra-args` (default: empty): optional space-separated extra flags appended to conformance `go test` after the fixed Ginkgo options (e.g. `-ginkgo.focus=Subsuite`), same idea as `./test/e2e/run-e2e.sh` forwarding `"$@"`.
- `catalog-url` (default: `https://github.com/konflux-ci/tekton-integration-catalog.git`): integration catalog repository.
- `catalog-revision` (default: pinned commit SHA): `tekton-integration-catalog` ref for catalog tasks; override to move to a different commit.

### Examples: `integration-go-test-extra-args` / `conformance-go-test-extra-args`

Params are a **single string**; the Task passes them into the shell **without extra quoting**, so **spaces separate flags** (same as typing multiple words after `go test` locally). Do not wrap the whole value in inner quotes unless you intend one literal argument.

**IntegrationTestScenario** — add under `spec.params` next to your other params:

```yaml
    - name: integration-go-test-extra-args
      value: "-run=TestKonfluxIntegration -count=1"
    - name: conformance-go-test-extra-args
      value: "-ginkgo.skip=Flaky -ginkgo.v=false"
```

**Pipelines as Code** — in the PipelineRun template, set param values (adjust to your PAC variable syntax):

```yaml
    - name: integration-go-test-extra-args
      value: ""
    - name: conformance-go-test-extra-args
      value: "-ginkgo.skip=Flaky"
```

**Direct `tkn` / YAML PipelineRun** — same shape: each param is one scalar string; use `=` style Ginkgo flags to avoid embedded spaces when possible.

## Expected Secret shapes

The pipeline passes **Secret names** as parameters. The **keys and formats** below match what **kind-aws-provision 0.2** and **kind-aws-deprovision 0.1** expect; open those task files at the same Git commit as `catalog-revision` in `pipeline.yaml` (for example [provision 0.2](https://github.com/konflux-ci/tekton-integration-catalog/blob/489cd0a413f52fd3fac90f38694f8fe51871be4a/tasks/mapt-oci/kind-aws-spot/provision/0.2/kind-aws-provision.yaml) and [deprovision 0.1](https://github.com/konflux-ci/tekton-integration-catalog/blob/489cd0a413f52fd3fac90f38694f8fe51871be4a/tasks/mapt-oci/kind-aws-spot/deprovision/0.1/kind-aws-deprovision.yaml) at the default pin). Re-verify whenever you bump `catalog-revision`.

### `oci-container-repo-credentials-secret` (registry auth for `oci-container-repo`)

- **Type:** `Opaque` is typical.
- **Required data key:** `oci-storage-dockerconfigjson` — value must be the **contents of a `.dockerconfigjson`** file (same JSON `docker`/`podman` use). In manifests, use `stringData` for the raw JSON body, or put base64-encoded content under `data`.
- The key name must be exactly `oci-storage-dockerconfigjson` (used by the catalog `secure-push-oci` step action).

Example (replace placeholders; prefer creating via `kubectl create secret generic` or your GitOps tool rather than committing raw credentials):

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-oci-push-secret
type: Opaque
stringData:
  # Body must be valid .dockerconfigjson; auth is base64("username:password").
  oci-storage-dockerconfigjson: '{"auths":{"quay.io":{"auth":"<base64(username:password)>"}}}'
```

### `aws-credentials-secret` and `deprovision-aws-credentials-secret`

Both reference the **same shape** of Secret unless your deprovision catalog task differs (this pipeline uses the same catalog family for both).

- **Type:** `Opaque`
- **Required data keys** (values are plain strings in `stringData`, or base64 in `data`):
  - `access-key` — AWS access key ID
  - `secret-key` — AWS secret access key
  - `region` — AWS region name (e.g. `us-east-1`)
  - `bucket` — S3 bucket name used by Mapt for this flow

Example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: konflux-mapt-us-east-1
type: Opaque
stringData:
  access-key: AKIA...
  secret-key: ...
  region: us-east-1
  bucket: my-mapt-bucket
```

## Resolution model

- The pipeline itself is intended to be git-resolved by PipelineRun (`pipelineRef.resolver: git`) from `.tekton/pipelines/operator-e2e/pipeline.yaml`.
- Catalog tasks are resolved via git resolver from `catalog-url`/`catalog-revision`.
- Local tasks (`deploy-konflux`, `konflux-e2e-tests`) are resolved via git resolver from `git-url`/`revision`.
- This allows external repos to reference this pipeline and pin which `konflux-ci` revision provides task logic.
