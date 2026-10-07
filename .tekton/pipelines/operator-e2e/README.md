# `operator-e2e-pipeline`

Tekton pipeline for operator E2E that provisions Kind on an IBM VSI, deploys Konflux, runs E2E tests, and deprovisions.

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
- `konflux-cr-configmap` (default: `konflux-deploy-cr`): optional ConfigMap in the PipelineRun namespace; when present, overrides the file from the clone.
- `konflux-cr-configmap-key` (default: `konflux-cr.yaml`): data key holding the Konflux CR YAML.
- `konflux-cr-relative-path` (default: `operator/config/samples/konflux-e2e.yaml`): CR path relative to the konflux-ci repo root when the ConfigMap is absent or the key is unset.
- `konflux-ready-timeout` (default: `30m`): readiness timeout for Konflux CR.
- `oci-container-repo` (required): OCI registry/repo prefix for kind-ibm provision/deprovision artifacts (logs/state); no tag suffix—the pipeline appends `:$(context.pipelineRun.name)` for provision/deprovision. The deploy task also pushes **post-prep** `operator/pkg/manifests` to the **same repo** with tag `$(context.pipelineRun.name).pkg-manifests` so it does not replace the provision artifact.
- `oci-container-repo-credentials-secret` (required): name of a Secret with registry credentials for `oci-container-repo` (kind-ibm `oci-credentials`). This repo’s PAC PipelineRun uses `konflux-test-infra`.
- `ibmcloud-credentials-secret` (default: `ibmcloud-mapt-credentials`): Secret containing IBM Cloud API and COS HMAC credentials.
- `region` (default: `us-south`): IBM Cloud region for the VSI.
- `zone` (default: `us-south-2`): IBM Cloud zone for the VSI.
- `provision-timeout` (default: `4h`): auto-destroy timeout for the IBM VSI. The provision task schedules destruction after this duration regardless of pipeline outcome, preventing orphaned resources when a PipelineRun is interrupted before the `finally` deprovision task runs (e.g., OOM, node eviction, manual cancellation). Choose a value that covers expected test duration plus buffer.
- `release-ta-oci-storage` (default: empty): optional OCI ref for conformance trusted-artifacts flow.
- `integration-go-test-extra-args` (default: empty): optional space-separated extra flags appended to integration `go test . ./pkg/...` (e.g. `-run=TestFoo -count=1`).
- `conformance-go-test-extra-args` (default: empty): optional space-separated extra flags appended to conformance `go test` after the fixed Ginkgo options (e.g. `-ginkgo.focus=Subsuite`), same idea as `./test/e2e/run-e2e.sh` forwarding `"$@"`.
- `conformance-image` (default: empty): optional conformance test image reference. When set, the conformance step runs the pre-built image as a pod (with kubeconfig and credentials injected) instead of compiling from source. The PAC PipelineRun sets this to the PR-built image to validate the conformance image as part of e2e. Timing note: the conformance image build fires in parallel with e2e, but Kind provisioning + Konflux deploy (~20 min) provides sufficient lead time for the image to be available.
- `catalog-url` (default: `https://github.com/konflux-ci/tekton-integration-catalog.git`): integration catalog repository.
- `catalog-revision` (default: pinned commit SHA): `tekton-integration-catalog` ref for catalog tasks; override to move to a different commit.

### Examples: Konflux CR (ConfigMap vs sample file)

**Precedence:** ConfigMap in the pipelineRun namespace → file at `konflux-cr-relative-path` in the konflux-ci clone. See `.tekton/tasks/deploy-konflux/README.md` for creating the ConfigMap.

**Custom CR via ConfigMap** (typical for another repo’s PipelineRun—no CR YAML on the PipelineRun):

```bash
kubectl create configmap konflux-deploy-cr \
  --from-file=konflux-cr.yaml=./my-konflux.yaml \
  -n konflux-vanguard-tenant
```

**Sample CR from the konflux-ci clone** (no ConfigMap, or delete `konflux-deploy-cr` in the namespace):

```yaml
    - name: konflux-cr-relative-path
      value: operator/config/samples/konflux_v1alpha1_konflux.yaml
```

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

The pipeline passes **Secret names** as parameters. The IBM task expects an `Opaque` Secret with the keys below; the same Secret is used for provision and deprovision.

The pipeline generates a short IBM-safe cluster ID from the PipelineRun UID. The same generated ID is used for the VSI name, resource tags, and the IBM COS state path, so concurrent runs across repositories do not collide.

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

### `ibmcloud-credentials-secret`

- **Type:** `Opaque`
- **Required data keys** (values are plain strings in `stringData`, or base64 in `data`):
  - `IBMCLOUD_API_KEY` — IBM Cloud API key
  - `IBMCLOUD_COS_ACCESS_KEY_ID` — COS HMAC access key
  - `IBMCLOUD_COS_SECRET_ACCESS_KEY` — COS HMAC secret key
  - `IBMCLOUD_COS_ENDPOINT` — optional COS endpoint override

Example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ibmcloud-mapt-credentials
type: Opaque
stringData:
  IBMCLOUD_API_KEY: ...
  IBMCLOUD_COS_ACCESS_KEY_ID: ...
  IBMCLOUD_COS_SECRET_ACCESS_KEY: ...
  IBMCLOUD_COS_ENDPOINT: ...
```

## Verifying task or pipeline changes

> **CI resolves `deploy-konflux` and `konflux-e2e-tests` Task YAML from `main`** in `pipeline.yaml` — tasks **`deploy-konflux-its`** and **`konflux-e2e-tests-its`** (`taskRef.params.revision: main`). Tekton cannot use PR/snapshot results in that resolver field.

If you change this pipeline or `.tekton/tasks/deploy-konflux/` / `.tekton/tasks/konflux-e2e-tests/`, temporarily set **both** `revision` values to a **git ref that contains your change** (branch or commit SHA), run operator E2E to check for regressions, then **restore `main` before merge**.

## Resolution model

- The pipeline itself is intended to be git-resolved by PipelineRun (`pipelineRef.resolver: git`) from `.tekton/pipelines/operator-e2e/pipeline.yaml`.
- Catalog tasks are resolved via git resolver from `catalog-url`/`catalog-revision`.
- Local tasks (`deploy-konflux`, `konflux-e2e-tests`) are resolved via git resolver from `git-url`/`revision` on the PAC path; the ITS path uses `main` for task YAML (see above).
- This allows external repos to reference this pipeline and pin which `konflux-ci` revision provides task logic.
