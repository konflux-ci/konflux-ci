# `deploy-konflux` Task

Deploys Konflux on a provisioned cluster and waits for `konflux/konflux` to become `Ready`.

## Inputs (params)

- `cluster-access-secret` (required): Secret name containing kubeconfig data for the target cluster.
- `kubeconfig-secret-key` (default: `kubeconfig`): key inside the Secret that contains kubeconfig bytes.
- `overrides-yaml` (default: empty): optional inline YAML consumed by `operator/cmd/overrides`.
- `konflux-ready-timeout` (default: `30m`): timeout for waiting on Konflux Ready condition.
- `konflux-cr-relative-path` (default: `operator/config/samples/konflux-e2e.yaml`): path to the Konflux CR YAML **relative to the repository root** of the clone (checkout is at `/mnt/konflux-ci/repo`).
- `kind-cluster-name` (default: `konflux-operator-e2e`): logical Kind cluster name passed to `deploy-local.sh`.

## Repository checkout (Task volumes)

This Task does **not** use Tekton `workspaces`. It clones `konflux-ci` into an `emptyDir` volume mounted at `/mnt/konflux-ci/repo` (see `task.yaml`).

## Required secrets / env sources

Reads keys from Secret `konflux-operator-e2e-credentials` in the Task namespace (for `deploy-prep` → `scripts/deploy-local.sh` / `deploy-secrets.sh`, matching `.github/workflows/operator-test-e2e.yaml` deploy step):

- `GITHUB_APP_ID`
- `GITHUB_PRIVATE_KEY`
- `WEBHOOK_SECRET`
- `QUAY_TOKEN`
- `QUAY_ORGANIZATION`
- `SMEE_CHANNEL`

## Steps / images

1. **fetch-kubeconfig** / **copy-shared-tools** — `quay.io/konflux-ci/task-runner:0.2.0` (pinned by digest in `task.yaml`).
2. **deploy-prep** — `registry.access.redhat.com/ubi10/go-toolset` (pinned by digest): optional overrides via `go run ./cmd/overrides`, then `deploy-local.sh` with `OPERATOR_INSTALL_METHOD=none` (needs credentials below).
3. **deploy-operator-and-wait** — `registry.access.redhat.com/ubi10/go-toolset` (pinned by digest): uses `kubectl`/`yq`/`jq` from `/mnt/e2e-shared/bin` and **`LD_LIBRARY_PATH=/mnt/e2e-shared/lib`** for `jq`, then `make` + out-of-cluster `bin/manager`, CR apply, Ready wait.

No on-the-fly tool downloads; shared CLIs are copied from task-runner.

## Notes

- This Task is Tekton `none`-mode oriented: it deploys dependencies, runs operator from source (`bin/manager`), applies Konflux CR, and waits for Ready.

## `overrides-yaml` examples

Use multiline YAML in the PipelineRun/TaskRun param value.

Example: image replacement only

```yaml
- name: segment-bridge
  images:
    - orig: quay.io/konflux-ci/segment-bridge
      replacement: quay.io/redhat-user-workloads/konflux-vanguard-tenant/segment-bridge/segment-bridge:on-pr-{{revision}}
```

Example: git override + image replacement

```yaml
- name: segment-bridge
  git:
    - sourceRepo: konflux-ci/segment-bridge
      remote:
        repo: https://github.com/konflux-ci/segment-bridge
        ref: {{revision}}
  images:
    - orig: quay.io/konflux-ci/segment-bridge
      replacement: quay.io/redhat-user-workloads/konflux-vanguard-tenant/segment-bridge/segment-bridge:on-pr-{{revision}}
```
