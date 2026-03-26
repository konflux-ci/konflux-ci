# Operator E2E helper scripts

Current shell entry points used by the Tekton operator E2E flow.

All scripts take the **konflux-ci repository root** as the first argument unless noted (absolute or relative path is fine).

## Tekton: operator lifecycle

With `OPERATOR_INSTALL_METHOD=none`, `tekton-deploy-operator-and-wait.sh` starts **`bin/manager` in the background** inside the **`deploy-konflux` Task** pod. When that Task completes, the pod is torn down and **that operator process ends**; it does not keep running on the cluster.

**`konflux-e2e-tests`** runs later, in **another pod**, and only uses **`kubectl` / `go test`** against the Kind cluster. **No Konflux operator** is running during that phase. In-cluster services the operator already installed (for example build-service) continue to run; tests should not assume the Konflux **operator** will reconcile or repair arbitrary API changes mid-suite.

For the full pipeline narrative, see **Scope** in `.tekton/pipelines/operator-e2e/README.md`.

## Extra `go test` arguments (integration / conformance)

The Tekton Task sets optional env vars from pipeline params (empty by default). Scripts **omit quotes** around those expansions on purpose: the shell must **split on spaces** so each flag becomes its own argument to `go test`. (Shellcheck rule SC2086 is disabled next to those lines because unquoted expansion is usually risky; here it is required for the same reason as forwarding `"$@"`.)

| Env var | Set by pipeline param | Appended to |
|--------|------------------------|-------------|
| `E2E_INTEGRATION_GO_TEST_EXTRA_ARGS` | `integration-go-test-extra-args` | `go test . ./pkg/...` |
| `E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS` | `conformance-go-test-extra-args` | `go test ./tests/conformance ...` (after fixed Ginkgo flags) |

**Local / ad hoc** (from repo root, same semantics as the Task):

```bash
export E2E_INTEGRATION_GO_TEST_EXTRA_ARGS='-run=TestKonfluxIntegration -count=1'
export E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS='-ginkgo.skip=Flaky'
bash scripts/operator-e2e/tekton-run-e2e-tests.sh "$(pwd)"
```

Use **one token per flag** where possible (e.g. `-ginkgo.skip=Flaky`). Values with spaces inside one flag are awkward in a flat env string; prefer Ginkgo’s `-name=value` form, or run `./test/e2e/run-e2e.sh` for full `"$@"` forwarding from your shell.

**Tekton** examples: see `.tekton/pipelines/operator-e2e/README.md` (pipeline params) and `.tekton/tasks/konflux-e2e-tests/README.md` (Task params).

## Scripts

| Script | Purpose |
|--------|---------|
| `prepare-conformance-env.sh` | Exports `CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE` from `operator/pkg/manifests/build-service/manifests.yaml` (same pin as GitHub Actions E2E). |
| `run-conformance-tests.sh` | Runs conformance tests. Requires `GH_ORG`, `GH_TOKEN` (conformance clears `QUAY_TOKEN` for the test run). |
| `tekton-fetch-kubeconfig.sh` | **Tekton:** decodes kubeconfig from Secret into `/mnt/e2e-shared/kubeconfig`. Args: `SECRET_NAME` `[KEY]`. Env: `POD_NAMESPACE`. |
| `tekton-copy-shared-tools.sh` | **Tekton:** copies `kubectl`, `yq`, `jq` into `/mnt/e2e-shared/bin` and **`jq`’s shared libraries** (`libjq`, `libonig`) into `/mnt/e2e-shared/lib` for `ubi10/go-toolset` steps. |
| `tekton-deploy-prep.sh` | **Tekton (go-toolset):** optional overrides via `operator/cmd/overrides`, then `deploy-local.sh` with `OPERATOR_INSTALL_METHOD=none`. |
| `tekton-push-operator-pkg-manifests-oci.sh` | **Tekton (task-runner):** after prep, push `operator/pkg/manifests` to OCI as `${oci-container-repo}:${pipelineRun}.pkg-manifests` (skips if `E2E_OCI_CONTAINER_REPO` blank). |
| `tekton-deploy-operator-and-wait.sh` | **Tekton (go-toolset):** `make install`/build, `bin/manager`, apply CR, wait Ready. |
| `tekton-run-e2e-tests.sh` | **Tekton:** waits for Konflux Ready, `deploy-test-resources.sh` (`SKIP_SAMPLE_COMPONENTS=true`), integration + conformance `go test`. Optional env: `E2E_INTEGRATION_GO_TEST_EXTRA_ARGS`, `E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS` (space-separated; pipeline params `integration-go-test-extra-args` / `conformance-go-test-extra-args` set these). |

## Override YAML schema

Each list item supports:

- `name` (component under `operator/upstream-kustomizations/`)
- `git` (array of rules; may be empty if only image overrides)
- `images` (array of `{ orig, replacement }`; may be empty if only git overrides)

At least one of `git` or `images` must be non-empty per item.

Each `git` rule:

- `sourceRepo`: `org/repo` or `https://github.com/org/repo`
- plus either:
  - `remote: { repo, ref }`
  - or `localPath`

`remote.ref` can be branch, tag, or SHA. First matching `sourceRepo` per resource URL wins.
