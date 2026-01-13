# notify-and-trigger-github-release Pipeline

Pipeline that sends Slack notifications and triggers GitHub release workflow.

## Description

This pipeline combines:
1. **Slack notification**: Uses the `notify-slack-on-failure` task from community-catalog
2. **GitHub repository_dispatch event**: Uses the `send-github-release-event` task
(only for tag-based releases)
3. **Failure notification**: Uses the `notify-slack-github-event-failure` task in the
`finally` block to notify if the GitHub event fails

The GitHub event task only runs for tag-based releases and gracefully exits for
branch-based releases. The failure notification task runs in the `finally` block,
ensuring it executes even if other tasks fail.

## Authentication

The pipeline supports two authentication methods for GitHub:

1. **GitHub App** (preferred): Provide a single secret (`githubAppSecretName`, default:
  `github-app-credentials`) with three keys:
   - `githubAppIdKey` (default: `app-id`) - contains the GitHub App ID
   - `githubInstallationIdKey` (default: `installation-id`) - contains the Installation ID
   - `githubPrivateKeyKey` (default: `private-key`) - contains the App's private key

2. **Personal Access Token (PAT)** (fallback): Provide `githubTokenSecretName` (default:
  `github-token`) and `githubTokenSecretKey`

**Priority**: GitHub App is checked first. If GitHub App credentials are not available,
the pipeline falls back to PAT. If neither is available, the task will fail.

## Parameters

| Name | Description | Optional | Default value |
|------|-------------|----------|---------------|
| release | Namespaced name of release (format: "namespace/name") | No | - |
| secretName | Name of secret which contains Slack webhook URL | No | - |
| secretKeyName | Name of key within secret which contains Slack webhook URL | No | - |
| slackHandles | Comma-separated list of Slack member IDs or group IDs to be tagged on failures | Yes | "" |
| notifySuccess | If "true", sends Slack notification on success | Yes | "false" |
| tagSuccess | If "true", tags users in success notifications | Yes | "false" |
| githubTokenSecretName | Name of secret which contains GitHub token (PAT). Used as fallback if GitHub App authentication is not available. | Yes | "github-token" |
| githubTokenSecretKey | Name of key within secret which contains GitHub token | Yes | token |
| githubAppSecretName | Name of secret which contains GitHub App credentials (app-id, installation-id, private-key). Preferred authentication method. | Yes | github-app-credentials |
| githubAppIdKey | Name of key within GitHub App secret which contains the App ID | Yes | app-id |
| githubInstallationIdKey | Name of key within GitHub App secret which contains the Installation ID | Yes | installation-id |
| githubPrivateKeyKey | Name of key within GitHub App secret which contains the private key | Yes | private-key |
| githubRepo | GitHub repository in format "owner/repo" | Yes | "konflux-ci/konflux-ci" |
| slackTaskGitUrl | URL to git repo where the Slack notification task is stored (community-catalog) | Yes | https://github.com/konflux-ci/community-catalog.git |
| slackTaskGitRevision | Revision in the Slack task git repo to be used | Yes | development |

## Task Execution Order

1. **notify-slack**: Always runs first
2. **send-github-event**: Runs after Slack notification succeeds (only for tag-based
  releases)
3. **notify-slack-github-event-failure** (in `finally` block): Runs if send-github-event
  task fails, regardless of overall pipeline status

## Usage in ReleasePlan

```yaml
spec:
  finalPipeline:
    params:
      - name: secretName
        value: vanguard-ci-notifier-webhook
      - name: secretKeyName
        value: url
      - name: slackHandles
        value: "Smygroup"
      # Option 1: Use GitHub App (preferred, uses defaults)
      # No params needed - uses default secret name "github-app-credentials"
      # with default keys: app-id, installation-id, private-key
      # Option 2: Use PAT token (fallback)
      # - name: githubTokenSecretName
      #   value: github-release-token
      # - name: githubTokenSecretKey
      #   value: token
      - name: githubRepo
        value: konflux-ci/konflux-ci
    pipelineRef:
      params:
        - name: url
          value: "https://github.com/konflux-ci/konflux-ci.git"
        - name: revision
          value: main
        - name: pathInRepo
          value: "pipelines/notify-and-trigger-github-release/notify-and-trigger-github-release.yaml"
      resolver: git
    serviceAccountName: build-pipeline-konflux-operator
```
