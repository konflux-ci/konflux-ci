# `deploy-konflux` Task

> **Changing this Task?** CI resolves its YAML from **`main`** (`deploy-konflux-its` in [`pipeline.yaml`](../pipelines/operator-e2e/pipeline.yaml)). Temporarily point that `revision` at your branch/SHA to verify regressions, then restore **`main`** before merge. See [operator-e2e README](../pipelines/operator-e2e/README.md#verifying-task-or-pipeline-changes).

Deploys Konflux on a provisioned cluster and waits for `konflux/konflux` to become `Ready`.

## Inputs (params)

- `cluster-access-secret` (required): Secret name containing kubeconfig data for the target cluster.
- `kubeconfig-secret-key` (default: `kubeconfig`): key inside the Secret that contains kubeconfig bytes.
- `overrides-yaml` (default: empty): optional inline YAML consumed by `operator/cmd/overrides`.
- `konflux-ready-timeout` (default: `30m`): timeout for waiting on Konflux Ready condition.
- `konflux-cr-configmap` (default: `konflux-deploy-cr`): optional ConfigMap in the Task namespace; when it exists and the key is set, its data takes precedence over `konflux-cr-relative-path` (see [Konflux CR inputs](#konflux-cr-inputs)).
- `konflux-cr-configmap-key` (default: `konflux-cr.yaml`): data key in that ConfigMap.
- `konflux-cr-relative-path` (default: `operator/config/samples/konflux-e2e.yaml`): path to Konflux CR YAML **relative to the repository root** of the clone when the ConfigMap is absent or the key is unset (checkout is at `/mnt/konflux-ci/repo`).
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

## Konflux CR inputs

The Task loads the CR via Tekton [`env` + `configMapKeyRef`](https://tekton.dev/docs/pipelines/tasks/#using-env) on `stepTemplate` (`optional: true`). The CR YAML is **not** stored on the PipelineRun—only the ConfigMap name and key params (defaults below).

### Precedence

1. **ConfigMap** (`konflux-cr-configmap` / `konflux-cr-configmap-key`) in the **Task namespace**, when the object exists and the key is non-empty → written to `/mnt/e2e-shared/konflux-cr.yaml` and applied.
2. **File in the clone** (`konflux-cr-relative-path` under the checked-out `git-url` / `revision`).

If a ConfigMap with the configured name exists in the namespace, it **always wins** over the sample file path—even when testing a konflux-ci PR that only changes `konflux-cr-relative-path`. Remove or rename the ConfigMap to use the file from the clone.

The document must be a complete `Konflux` resource (`kind: Konflux`, `metadata.name: konflux`). ConfigMaps are readable by anyone with `get configmap` in the namespace; do not put secrets in the CR (use normal cluster secrets for credentials).

### Create the ConfigMap (before the PipelineRun)

Defaults match the task params: name `konflux-deploy-cr`, data key `konflux-cr.yaml`.

**From a file** (recommended — copy a sample and edit):

```bash
cp operator/config/samples/konflux-e2e.yaml /tmp/konflux-cr.yaml
# edit /tmp/konflux-cr.yaml (e.g. webhookURLs — see webhook URL configuration guide)
kubectl create configmap konflux-deploy-cr \
  --from-file=konflux-cr.yaml=/tmp/konflux-cr.yaml \
  -n <task-namespace>
```

**Manifest example** (structure only; complete the `spec` from [`konflux-e2e.yaml`](https://github.com/konflux-ci/konflux-ci/blob/main/operator/config/samples/konflux-e2e.yaml) or another sample):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: konflux-deploy-cr
  namespace: <task-namespace>
data:
  konflux-cr.yaml: |
    apiVersion: konflux.konflux-ci.dev/v1alpha1
    kind: Konflux
    metadata:
      name: konflux
    spec:
      ui:
        spec:
          ingress:
            nodePortService:
              httpsPort: 30011
      # ... remainder of spec (buildService, imageController, etc.)
```

Apply with `kubectl apply -f konflux-deploy-cr-configmap.yaml`.

Override the name or key on the PipelineRun if needed:

```yaml
- name: konflux-cr-configmap
  value: my-team-konflux-cr
- name: konflux-cr-configmap-key
  value: konflux-cr.yaml
```

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
