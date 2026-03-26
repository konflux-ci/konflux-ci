# `konflux-e2e-tests` Task

Runs Konflux E2E test phases against an already deployed Konflux instance.

## Inputs (params)

- `cluster-access-secret` (required): Secret name containing kubeconfig data for the target cluster.
- `kubeconfig-secret-key` (default: `kubeconfig`): key inside the Secret that contains kubeconfig bytes.
- `konflux-ready-timeout` (default: `30m`): timeout used while waiting for Konflux CR readiness before tests start.
- `release-ta-oci-storage` (default: empty): optional OCI location passed to conformance trusted-artifacts flow.

## Required workspace

- `source`: cloned `konflux-ci` repository.

## Required secrets / env sources

Reads keys from Secret `konflux-operator-e2e-credentials` in the Task namespace:

- `GH_ORG`
- `GH_TOKEN`
- `QUAY_DOCKERCONFIGJSON`
- `RELEASE_CATALOG_TA_QUAY_TOKEN`

## Steps / images

1. **fetch-kubeconfig** / **copy-shared-tools** — `quay.io/konflux-ci/task-runner:0.2.0` (copies `kubectl`/`yq`/`jq` to `/mnt/e2e-shared/bin`).
2. **run-tests** — `registry.access.redhat.com/ubi10/go-toolset`: integration + conformance (`go test`), plus `kubectl`/`curl` from shared bin and image.

## Notes

- This Task waits for the `konflux/konflux` resource to exist and be `Ready`, then runs:
  - `deploy-test-resources.sh` (with `SKIP_SAMPLE_COMPONENTS=true`)
  - `go test . ./pkg/...` under `test/go-tests` (integration)
  - conformance tests (`run-conformance-tests.sh`)
