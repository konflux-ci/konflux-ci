# Operator E2E helper scripts

Current shell entry points used by the Tekton operator E2E flow.

All scripts take the **konflux-ci repository root** as the first argument unless noted (absolute or relative path is fine).

## Scripts

| Script | Purpose |
|--------|---------|
| `apply-overrides.sh` | Low-level override applier. Input via `COMPONENT_SOURCES_FILE` (`name` + `git[]`) and optional `IMAGE_OVERRIDES`. Requires `yq`, `jq`, `kustomize`. |
| `apply-overrides-from-yaml.sh` | Reads override YAML (`OVERRIDES_YAML` or `OVERRIDES_YAML_PATH`) -> `.tmp/component-sources.json` -> `apply-overrides.sh`. |
| `post-deploy-sanity.sh` | Prints `kubectl` and optional `kind` versions and namespaces (kind skipped if absent). |
| `run-deploy-test-resources.sh` | Runs `./deploy-test-resources.sh` from repo root. Honors `SKIP_SAMPLE_COMPONENTS` (defaults to `true` in script). |
| `run-integration-tests.sh` | Runs `go test . ./pkg/...` under `test/go-tests`. |
| `prepare-conformance-env.sh` | Exports `RELEASE_SERVICE_CATALOG_REVISION` and `CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE`. |
| `run-conformance-tests.sh` | Runs conformance tests. Requires `GH_ORG`, `GH_TOKEN`, `QUAY_DOCKERCONFIGJSON`, `RELEASE_CATALOG_TA_QUAY_TOKEN`. |
| `tekton-fetch-kubeconfig.sh` | **Tekton:** decodes kubeconfig from Secret into `/mnt/e2e-shared/kubeconfig`. Args: `SECRET_NAME` `[KEY]`. Env: `POD_NAMESPACE`. |
| `tekton-copy-shared-tools.sh` | **Tekton:** copies `kubectl`, `yq`, `jq` from task-runner into `/mnt/e2e-shared/bin` for go-toolset steps. |
| `tekton-kubectl-kustomize.sh` | **Tekton:** source-only; defines `kustomize()` as `kubectl kustomize` for `apply-overrides.sh`. |
| `tekton-deploy-prep.sh` | **Tekton (task-runner):** overrides + `deploy-local.sh` with `OPERATOR_INSTALL_METHOD=none`. |
| `tekton-deploy-operator-and-wait.sh` | **Tekton (go-toolset):** `make install`/build, `bin/manager`, apply CR, wait Ready. |
| `tekton-run-e2e-tests.sh` | **Tekton:** test-phase script (uses `/mnt/e2e-shared/bin` for kubectl). |

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
