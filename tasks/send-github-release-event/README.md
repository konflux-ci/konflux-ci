# send-github-release-event

Tekton task to send a `repository_dispatch` event to GitHub after a successful tag-based
release.

## Description

This task extracts version, image tag, and git ref from the Release CR and sends a
`repository_dispatch` event to trigger the GitHub release workflow.

The task only runs for tag-based releases (when `source-branch` starts with
`refs/tags/`). It gracefully exits if the release is not tag-based or if the release was
not successful.

## Authentication

The task supports two authentication methods:

1. **GitHub App** (preferred): Provide a single secret (`githubAppSecretName`, default:
   `github-app-credentials`) with three keys:
   - `githubAppIdKey` (default: `app-id`) - contains the GitHub App ID
   - `githubInstallationIdKey` (default: `installation-id`) - contains the Installation ID
   - `githubPrivateKeyKey` (default: `private-key`) - contains the App's private key

   The task will generate a JWT, exchange it for an installation token, and use that token.

2. **Personal Access Token (PAT)** (fallback): Provide `githubTokenSecretName`
   (default: `github-token`) and `githubTokenSecretKey`. The token is read directly
   from the secret.

**Priority**: GitHub App is checked first. If GitHub App credentials are not available,
the task falls back to PAT. If neither is available, the task will fail.

## Parameters

| Name | Description | Optional | Default value |
|------|-------------|----------|---------------|
| release | The namespaced name of the Release (format: "namespace/name") | No | - |
| githubTokenSecretName | Name of secret which contains GitHub token (PAT). Used as fallback if GitHub App authentication is not available. | Yes | "github-token" |
| githubTokenSecretKey | Name of key within secret which contains GitHub token | Yes | token |
| githubAppSecretName | Name of secret which contains GitHub App credentials (app-id, installation-id, private-key). Preferred authentication method. | Yes | github-app-credentials |
| githubAppIdKey | Name of key within GitHub App secret which contains the App ID | Yes | app-id |
| githubInstallationIdKey | Name of key within GitHub App secret which contains the Installation ID | Yes | installation-id |
| githubPrivateKeyKey | Name of key within GitHub App secret which contains the private key | Yes | private-key |
| githubRepo | GitHub repository in format "owner/repo" (e.g., "konflux-ci/konflux-ci") | No | - |

## Behavior

1. **Checks release status**: Only proceeds if release status is "True"
2. **Checks if tag-based**: Only proceeds if `source-branch` starts with `refs/tags/`
3. **Extracts data from Release CR**:
   - Version: Extracted from `source-branch` annotation (removes `refs/tags/` prefix)
   - Git SHA: From `metadata.labels.pac.test.appstudio.openshift.io/sha`
   - Image tag: From `status.artifacts.images[0].urls[]` (finds `release-sha-*` format)
4. **Sends GitHub event**: Sends `repository_dispatch` with event type `konflux-build-complete`

## Exit Codes

- **0 (success)**: Event sent successfully, or release was not tag-based (graceful exit)
- **1 (failure)**: Missing required parameters or secrets

The task is designed to not fail the pipeline if the GitHub event cannot be sent (e.g.,
network issues). It logs a warning and exits successfully.
