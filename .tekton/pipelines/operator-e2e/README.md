# `operator-e2e-pipeline`

Tekton pipeline for operator E2E that provisions Kind on AWS, deploys Konflux, runs E2E tests, and deprovisions.

Current scope in this branch:

- Tekton flow is `none`-mode oriented for operator install (out-of-cluster `bin/manager` in deploy task).
- Deploy and tests are split across `deploy-konflux` and `konflux-e2e-tests`.

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
- `catalog-url` (default: `https://github.com/konflux-ci/tekton-integration-catalog.git`): integration catalog repository.
- `catalog-revision` (default: pinned commit SHA): `tekton-integration-catalog` ref for catalog tasks; override to move to a different commit.

## Workspaces

- `source`: shared repo workspace across clone/deploy/tests tasks.
- `git-auth`: optional/basic-auth workspace for git clone credentials.

## Expected Secret and workspace shapes

The pipeline only passes **Secret names** (or binds a workspace to a Secret). The **keys and formats** below match what **kind-aws-provision 0.2** and **kind-aws-deprovision 0.1** expect; open those task files at the same Git commit as `catalog-revision` in `pipeline.yaml` (for example [provision 0.2](https://github.com/konflux-ci/tekton-integration-catalog/blob/489cd0a413f52fd3fac90f38694f8fe51871be4a/tasks/mapt-oci/kind-aws-spot/provision/0.2/kind-aws-provision.yaml) and [deprovision 0.1](https://github.com/konflux-ci/tekton-integration-catalog/blob/489cd0a413f52fd3fac90f38694f8fe51871be4a/tasks/mapt-oci/kind-aws-spot/deprovision/0.1/kind-aws-deprovision.yaml) at the default pin). Re-verify whenever you bump `catalog-revision`.

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

### `git-auth` workspace

Bound to a `Secret` (PAC often sets `secretName` from `git_auth_secret`). The **git-clone** task accepts several layouts; common ones:

- **`kubernetes.io/basic-auth`:** keys `username` and `password` (UTF-8), or
- **Opaque:** files `.git-credentials` and `.gitconfig` copied as in the [git-clone task](https://github.com/konflux-ci/build-definitions/blob/main/task/git-clone/0.1/git-clone.yaml) description (see `basic-auth` workspace).

If the workspace is not bound, clone may still work for public `git-url` repos.

## Resolution model

- The pipeline itself is intended to be git-resolved by PipelineRun (`pipelineRef.resolver: git`) from `.tekton/pipelines/operator-e2e/pipeline.yaml`.
- Catalog tasks are resolved via git resolver from `catalog-url`/`catalog-revision`.
- Local tasks (`deploy-konflux`, `konflux-e2e-tests`) are resolved via git resolver from `git-url`/`revision`.
- This allows external repos to reference this pipeline and pin which `konflux-ci` revision provides task logic.
