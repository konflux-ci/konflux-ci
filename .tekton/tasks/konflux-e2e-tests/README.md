# `konflux-e2e-tests` Task

Runs Konflux E2E test phases against an already deployed Konflux instance.

## Inputs (params)

- `cluster-access-secret` (required): Secret name containing kubeconfig data for the target cluster.
- `kubeconfig-secret-key` (default: `kubeconfig`): key inside the Secret that contains kubeconfig bytes.
- `konflux-ready-timeout` (default: `30m`): timeout used while waiting for Konflux CR readiness before tests start.
- `release-ta-oci-storage` (default: empty): optional OCI location passed to conformance trusted-artifacts flow.
- `integration-go-test-extra-args` (default: empty): optional space-separated flags appended to `go test . ./pkg/...`.
- `conformance-go-test-extra-args` (default: empty): optional space-separated flags appended to conformance `go test` after `-ginkgo.junit-report=...` (e.g. `-ginkgo.focus=...`).

These map to env vars `E2E_INTEGRATION_GO_TEST_EXTRA_ARGS` and `E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS` on the `run-tests` step. The helper scripts expand them **unquoted** so each token becomes a separate `go test` argument (Shellcheck SC2086 is intentionally disabled there).

### Examples (param values)

```yaml
# Integration only: narrow packages/tests
integration-go-test-extra-args: "-run=TestFoo -count=1"

# Conformance: skip a label, reduce Ginkgo verbosity (fixed flags still add -ginkgo.vv unless you override)
conformance-go-test-extra-args: "-ginkgo.skip=Upstream -ginkgo.v=false"
```

Prefer **`-flag=value`** forms for Ginkgo when values might contain spaces. See also `scripts/operator-e2e/README.md` § “Extra go test arguments”.

## Repository checkout (Task volumes)

This Task does **not** use Tekton `workspaces`. It clones `konflux-ci` into an `emptyDir` volume mounted at `/mnt/konflux-ci/repo` (see `task.yaml`).

## Required secrets / env sources

Reads keys from Secret `konflux-operator-e2e-credentials` in the Task namespace:

- `GH_ORG`
- `GH_TOKEN`

## Steps / images

1. **fetch-kubeconfig** / **copy-shared-tools** — `quay.io/konflux-ci/task-runner:0.2.0` (copies `kubectl`/`yq`/`jq` to `/mnt/e2e-shared/bin` and `jq`’s `.so` deps to `/mnt/e2e-shared/lib`).
2. **run-tests** — `registry.access.redhat.com/ubi10/go-toolset`: integration + conformance (`go test`), plus `kubectl`/`curl` from shared bin and image.

## Notes

- This Task waits for the `konflux/konflux` resource to exist and be `Ready`, then runs:
  - `deploy-test-resources.sh` (with `SKIP_SAMPLE_COMPONENTS=true`)
  - `go test . ./pkg/...` under `test/go-tests` (integration)
  - conformance tests (`run-conformance-tests.sh`)
