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
- `oci-container-repo` (default: `quay.io/example/konflux-operator-e2e-artifacts`): OCI prefix used by provision/deprovision tasks.
- `konflux-test-infra-secret` (default: `konflux-test-infra`): secret for OCI credentials.
- `aws-credentials-secret` (default: `konflux-mapt-us-east-1`): AWS credentials for provision task.
- `deprovision-aws-credentials-secret` (default: `konflux-mapt-us-east-1`): AWS credentials for deprovision task.
- `release-ta-oci-storage` (default: empty): optional OCI ref for conformance trusted-artifacts flow.
- `catalog-url` (default: `https://github.com/konflux-ci/tekton-integration-catalog.git`): integration catalog repository.
- `catalog-revision` (default: pinned commit SHA): `tekton-integration-catalog` ref for catalog tasks; override to move to a different commit.

## Workspaces

- `source`: shared repo workspace across clone/deploy/tests tasks.
- `git-auth`: optional/basic-auth workspace for git clone credentials.

## Resolution model

- The pipeline itself is intended to be git-resolved by PipelineRun (`pipelineRef.resolver: git`) from `.tekton/pipelines/operator-e2e/pipeline.yaml`.
- Catalog tasks are resolved via git resolver from `catalog-url`/`catalog-revision`.
- Local tasks (`deploy-konflux`, `konflux-e2e-tests`) are resolved via git resolver from `git-url`/`revision`.
- This allows external repos to reference this pipeline and pin which `konflux-ci` revision provides task logic.
