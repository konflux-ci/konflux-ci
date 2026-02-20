# Release Process

<!-- toc -->

- [Automated Release Flow](#automated-release-flow)
  * [Step 1: Auto-Tag Creation](#step-1-auto-tag-creation)
  * [Step 2: Konflux Build and Release](#step-2-konflux-build-and-release)
  * [Step 3: GitHub Release Creation](#step-3-github-release-creation)
  * [Step 4: Community Operator Publication](#step-4-community-operator-publication)
- [Manual Releases](#manual-releases)
  * [On-Demand Release (Create New Tag)](#on-demand-release-create-new-tag)
  * [Manual Release (Existing Tag and Image)](#manual-release-existing-tag-and-image)
  * [Manual Community Operator PR](#manual-community-operator-pr)
- [Release Artifacts](#release-artifacts)
- [Troubleshooting](#troubleshooting)
- [Related Documentation](#related-documentation)

<!-- tocstop -->

Tags pushed to this repository trigger an automated weekly release process.
The process involves four main steps:

1. **Auto-tagging**: A GitHub Actions workflow automatically creates a new tag
   on the main branch
2. **Build and Release**: Konflux builds the operator image and releases it to
   Quay, then sends an event back to GitHub
3. **GitHub Release Creation**: A GitHub Actions workflow creates the GitHub
   release with artifacts
4. **Community Operator Publication**: A GitHub Actions workflow creates a PR to
   the Red Hat Community Operators repository to publish the operator in the
   OpenShift catalog

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

## Step 4: Community Operator Publication

When a GitHub release is published, the
[Community Operator PR workflow](.github/workflows/community-operator-pr.yaml)
automatically creates a pull request to the
[Red Hat Community Operators repository](https://github.com/redhat-openshift-ecosystem/community-operators-prod)
to publish the Konflux operator in the OpenShift catalog.

The workflow:

- Downloads the OLM bundle files (`bundle.tar.gz`) from the release artifacts
- Extracts the bundle contents (manifests, metadata, Dockerfile, release-config.yaml)
- Creates a PR to [operators/konflux](https://github.com/redhat-openshift-ecosystem/community-operators-prod/tree/main/operators/konflux)
  in the community-operators-prod repository

**Catalog Configuration:**

- The `release-config.yaml` file (generated in
  [generate-release-artifacts.sh](.github/scripts/generate-release-artifacts.sh))
  specifies the OLM channel (`Stable`) for the bundle
- The [ci.yaml](https://github.com/redhat-openshift-ecosystem/community-operators-prod/blob/main/operators/konflux/ci.yaml)
  file in the upstream repository defines which OpenShift catalog versions the
  operator is published to (catalog versions correspond to OpenShift versions)

**Adding Support for a New OpenShift Version:**

To publish the operator to a new OpenShift catalog version, update the
[ci.yaml](https://github.com/redhat-openshift-ecosystem/community-operators-prod/blob/main/operators/konflux/ci.yaml)
file in the community-operators-prod repository by adding the new version to the
`catalog_names` list:

```yaml
fbc:
  enabled: true
  catalog_mapping:
    - template_name: semver.yaml
      type: olm.semver
      catalog_names:
        - v4.20
        - v4.21  # Add new OpenShift version here
```

This change must be submitted as a PR to the
[community-operators-prod](https://github.com/redhat-openshift-ecosystem/community-operators-prod)
repository.

**Example PR:** [community-operators-prod#8626](https://github.com/redhat-openshift-ecosystem/community-operators-prod/pull/8626)

For more information about the community catalog automation and FBC (File-Based
Catalog) workflow, see the
[Operator Pipelines documentation](https://redhat-openshift-ecosystem.github.io/operator-pipelines/).

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

## Manual Community Operator PR

If a GitHub release already exists and you need to manually trigger the
community operator PR (e.g., if the automatic trigger failed), you can use the
[Community Operator PR workflow](.github/workflows/community-operator-pr.yaml)
via `workflow_dispatch`. You must provide:

- **release_tag**: GitHub release tag (e.g., `v0.0.4`)

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
- [Community Operator PR Workflow](.github/workflows/community-operator-pr.yaml)
- [Create Community Operator PR Script](.github/scripts/create-community-operator-pr.sh)
- [Operator Pipelines Documentation](https://redhat-openshift-ecosystem.github.io/operator-pipelines/) (external)
- [Konflux Operator in Community Catalog](https://github.com/redhat-openshift-ecosystem/community-operators-prod/tree/main/operators/konflux) (external)
