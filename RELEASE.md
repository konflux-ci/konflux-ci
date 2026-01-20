# Release Process

<!-- toc -->

- [Automated Release Flow](#automated-release-flow)
  * [Step 1: Auto-Tag Creation](#step-1-auto-tag-creation)
  * [Step 2: Konflux Build and Release](#step-2-konflux-build-and-release)
  * [Step 3: GitHub Release Creation](#step-3-github-release-creation)
- [Manual Releases](#manual-releases)
  * [On-Demand Release (Create New Tag)](#on-demand-release-create-new-tag)
  * [Manual Release (Existing Tag and Image)](#manual-release-existing-tag-and-image)
- [Release Artifacts](#release-artifacts)
- [Troubleshooting](#troubleshooting)
- [Related Documentation](#related-documentation)

<!-- tocstop -->

This repository uses an automated release process that creates weekly releases
when tags are pushed to the repository. The process involves three main steps:

1. **Auto-tagging**: A GitHub Actions workflow automatically creates a new tag
   on the main branch
2. **Build and Release**: Konflux builds the operator image and releases it to
   Quay, then sends an event back to GitHub
3. **GitHub Release Creation**: A GitHub Actions workflow creates the GitHub
   release with artifacts

# Automated Release Flow

## Step 1: Auto-Tag Creation

The [Auto Tag Weekly workflow](.github/workflows/auto-tag-weekly.yaml) runs
automatically every week (or can be triggered manually via `workflow_dispatch`).
This workflow:

- Checks if HEAD is already tagged (skips if already tagged)
- Finds the latest semantic version tag (format: `vX.Y.Z`)
- Increments the patch version (Z number)
- Creates and pushes the new tag to the repository

**Example**: If the latest tag is `v0.1.5`, the workflow will create `v0.1.6`.

## Step 2: Konflux Build and Release

When a new tag is pushed, the
[konflux-operator-tag PipelineRun](.tekton/konflux-operator-tag.yaml) is
automatically triggered in Konflux. This PipelineRun:

- Builds the operator image for multiple platforms (linux/x86_64, linux/arm64)
- Triggers a release of the image to Quay.io
- Triggers the final pipeline which includes the
  [notify-and-trigger-github-release pipeline](pipelines/notify-and-trigger-github-release/notify-and-trigger-github-release.yaml)

The final pipeline uses the
[send-github-release-event task](tasks/send-github-release-event/send-github-release-event.yaml)
to send a `repository_dispatch` event back to GitHub with the release
information (version, image tag, and git ref).

## Step 3: GitHub Release Creation

The `repository_dispatch` event triggers the
[Create Release workflow](.github/workflows/create-release.yaml), which:

- Extracts the release information from the event payload
- Checks out the target commit (git ref)
- Generates release artifacts (install.yaml, samples.tar.gz, bundle.tar.gz)
- Prepares release notes
- Creates the GitHub release with all artifacts

# Manual Releases

The release process can be triggered on demand in two ways:

## On-Demand Release (Create New Tag)

You can trigger the release process on demand by manually running the
[Auto Tag Weekly workflow](.github/workflows/auto-tag-weekly.yaml) via
`workflow_dispatch`. This will create a new tag and trigger the full automated
release flow (build, release to Quay, and create GitHub release).

## Manual Release (Existing Tag and Image)

If a tag already exists and the corresponding image is already present in Quay,
you can create a GitHub release directly using the
[Create Release workflow](.github/workflows/create-release.yaml) via
`workflow_dispatch`. When triggered manually, you must provide:

- **version**: Release version (e.g., `v0.0.1`)
- **git_ref**: Git ref to release (commit SHA, branch, or tag)
- **image_tag**: Image tag (e.g., `release-sha-abc1234`)

Optional parameters:
- **draft**: Create as draft release (default: `false`)
- **generate_notes**: Include auto-generated notes with commit history (default:
  `true`)

# Release Artifacts

Each release includes the following artifacts:

- **install.yaml**: Complete installation manifest (includes CRDs, RBAC, and
  operator deployment)
- **samples.tar.gz**: Sample Custom Resources
- **bundle.tar.gz**: OLM bundle with Dockerfile and release-config.yaml
- **version**: Plain text file with the release version

# Troubleshooting

If a release fails, the workflow will automatically create or update a GitHub
issue with failure details. Check the workflow run logs and the created issue
for more information.

# Related Documentation

- [Auto Tag Weekly Workflow](.github/workflows/auto-tag-weekly.yaml)
- [Konflux Operator Tag PipelineRun](.tekton/konflux-operator-tag.yaml)
- [Create Release Workflow](.github/workflows/create-release.yaml)
- [Notify and Trigger GitHub Release Pipeline](pipelines/notify-and-trigger-github-release/notify-and-trigger-github-release.yaml)
- [Send GitHub Release Event Task](tasks/send-github-release-event/send-github-release-event.yaml)
