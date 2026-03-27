# Release Process

<!-- toc -->

- [Periodic Releases](#periodic-releases)
  * [Step 1: Auto-Tag Creation](#step-1-auto-tag-creation)
  * [Step 2: Konflux Build and Release](#step-2-konflux-build-and-release)
  * [Step 3: GitHub Release Creation](#step-3-github-release-creation)
  * [Step 4: Community Operator Publication](#step-4-community-operator-publication)
- [Manual Steps](#manual-steps)
  * [Cut a New Release Stream](#cut-a-new-release-stream)
  * [Promote Release Candidate](#promote-release-candidate)
  * [On-Demand Release (Create New Tag)](#on-demand-release-create-new-tag)
  * [Manual Release (Existing Tag and Image)](#manual-release-existing-tag-and-image)
  * [Manual Community Operator PR](#manual-community-operator-pr)
- [Release Artifacts](#release-artifacts)
- [Troubleshooting](#troubleshooting)
- [Related Documentation](#related-documentation)

<!-- tocstop -->

This repository uses an automated release process that creates weekly release
candidates across all active release branches. The process involves four main
steps:

1. **Auto-tagging**: A GitHub Actions workflow automatically creates a new RC
   tag on every active release branch (`main` and `release-x.y` branches).
   Branches can be excluded from tagging and release verification via an
   [exclusion list](.github/excluded-release-branches.yaml).
2. **Build and Release**: Konflux builds the operator image via the `on-tag`
   pipeline and triggers the `release` and `finalPipeline` stages
3. **GitHub Release Creation**: A GitHub Actions workflow creates either a
   pre-release or a full release depending on the tag format
4. **Community Operator Publication**: A GitHub Actions workflow creates a PR to
   the Red Hat Community Operators repository to publish the operator in the
   OpenShift catalog

# Periodic Releases

## Step 1: Auto-Tag Creation

The [Auto Tag Weekly workflow](.github/workflows/auto-tag-weekly.yaml) runs
automatically every week (or can be triggered manually via `workflow_dispatch`).
This workflow runs against every active release branch (`main` and all
`release-x.y` branches) and for each branch:

- Increments the RC tag if the latest tag on that branch is already an RC tag
- Creates a new `rc.0` tag if the latest tag on the branch is a proper release

**Examples**:
- Latest tag `v0.1.5-rc.2` → creates `v0.1.5-rc.3`
- Latest tag `v0.1.5` → creates `v0.1.6-rc.0`

## Step 2: Konflux Build and Release

When a new tag is pushed, the
[konflux-operator-tag PipelineRun](.tekton/konflux-operator-tag.yaml) (`on-tag`
pipeline) is automatically triggered in Konflux. The pipeline chain is:

1. **`on-tag` pipeline** — builds the operator image for multiple platforms
   (linux/x86_64, linux/arm64) and triggers the `release` pipeline
2. **`release` pipeline** — releases the image to Quay.io and triggers the
   `finalPipeline`
3. **`finalPipeline`** — uses the
   [send-github-release-event task](tasks/send-github-release-event/send-github-release-event.yaml)
   to send a `repository_dispatch` event back to GitHub with the release
   information (version, image tag, and git ref)

## Step 3: GitHub Release Creation

The `repository_dispatch` event triggers the
[Create Release workflow](.github/workflows/create-release.yaml), which:

- Extracts the release information from the event payload
- Checks out the target commit (git ref)
- Generates release artifacts (install.yaml, samples.tar.gz, bundle.tar.gz)
- Prepares release notes
- Creates the GitHub release — as a **pre-release** if the tag is an RC tag
  (e.g., `vX.Y.Z-rc.W`), or as a **full release** if the tag is a proper
  release (e.g., `vX.Y.Z`)

## Step 4: Community Operator Publication

When a GitHub release is published, the
[Community Operator PR workflow](.github/workflows/community-operator-pr.yaml)
creates a **draft** pull request to the
[Red Hat Community Operators repository](https://github.com/redhat-openshift-ecosystem/community-operators-prod).
Draft PRs are used so that only one catalog PR is mergeable at a time (when multiple
releases from different branches create PRs in parallel, marking them all ready would
cause conflicts in the downstream catalog-update PRs).

For the operator to be published to the OpenShift catalog, a draft PR must be marked
**Ready for review**.

An automated workflow is periodically checking the status of existing PRs and rebasing
them one at a time and then marking them as ready. With no manual intervention, one PR
will be marked as ready per day.

The workflow:

- Downloads the OLM bundle files (`bundle.tar.gz`) from the release artifacts
- Extracts the bundle contents (manifests, metadata, Dockerfile, release-config.yaml)
- Creates a PR to [operators/konflux](https://github.com/redhat-openshift-ecosystem/community-operators-prod/tree/main/operators/konflux)
  in the community-operators-prod repository

**Catalog Configuration:**

- The `release-config.yaml` file (generated in
  [generate-release-artifacts.sh](.github/scripts/generate-release-artifacts.sh))
  specifies the OLM channel for the bundle: `Stable` for stable releases,
  `Candidate` for release-candidate (prerelease) versions (e.g. tags containing `rc`).
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

# Manual Steps

## Cut a New Release Stream

Use this procedure when starting development in a new y-stream (e.g.,
moving from `v0.1.x` to `v0.2.x`, or from `v1.y.x` to `v2.0.x`).

1. Create a development stream and RPA in the `releng` repository:
 * [development stream](https://gitlab.cee.redhat.com/releng/konflux-release-data/-/tree/main/tenants-config/cluster/stone-prd-rh01/tenants/konflux-vanguard-tenant/konflux-operator/_base/projectl.konflux.dev)
 * [release plan admission](https://gitlab.cee.redhat.com/releng/konflux-release-data/-/tree/main/config/stone-prd-rh01.pg1f.p1/service/ReleasePlanAdmission/konflux-vanguard)


2. Manually trigger the
   [Create Release Branch workflow](.github/workflows/create-release-branch.yaml)
   via `workflow_dispatch`, providing:
   - **dev version** (`x.y`): the version to be used going forward on `main`
   - **release version** (`x'.y'`): the version to be used on the new
     `release-x'-y'` branch

   The workflow will create the `release-x'-y'` branch and automatically open a
   PR on `main` that updates the `on-tag` CEL expression to target the new
   development version.

   **Example**:
   Suppose `main` was used for developing `v4.5.x` and you want to
   start working on the `4.6` y-stream while preparing to release `v4.5.0`.
   Set **dev version** to `4.6` (used on `main` going forward) and **release
   version** to `4.5` (used on the new `release-4-5` branch).

   After triggering, verify that the new component in Konflux is functional. If
   the component shows the following status:

   ```
   build.appstudio.openshift.io/status: >-
     {"message":"waiting for spec.containerImage to be set by ImageRepository
     with annotation image-controller.appstudio.redhat.com/update-component-image"}
   ```

   Patch the `imagerepository` resource to unblock it (replace `X` and `Y` with
   the release version numbers):

   ```bash
   REPO=konflux-operator-X-Y
   kubectl patch imagerepository $REPO -n konflux-vanguard-tenant --type=merge \
     -p '{"metadata":{"annotations":{"image-controller.appstudio.redhat.com/update-component-image":"true"}}}'
   ```

3. Merge the PR automatically created by the workflow in step 2 (via
   [create-release-branch-and-pr.sh](.github/scripts/create-release-branch-and-pr.sh)).

4. Configure branch protection rules for the new `release-x'-y'` branch as
   needed.

## Promote Release Candidate

Use this procedure to promote an existing RC tag to a proper release.

1. Manually trigger the [Promote Release workflow](https://github.com/konflux-ci/konflux-ci/actions/workflows/promote-release.yaml) via `workflow_dispatch`, providing:
   - **release_candidate_tag**: The release candidate tag to promote (e.g., `vX.Y.Z-rc.W`)

   The workflow will tag the same commit with the corresponding proper release
   tag (`vX.Y.Z`), which triggers the full automated release flow (build,
   release to Quay, create GitHub release, and community operator PR).

## On-Demand Release (Create New Tag)

You can trigger the release process on demand by manually running the
[Auto Tag Weekly workflow](.github/workflows/auto-tag-weekly.yaml) via
`workflow_dispatch`. This will create a new RC tag on all active release
branches and trigger the full automated release flow.

## Manual Release (Existing Tag and Image)

If a tag already exists and the corresponding image is already present in Quay,
you can create a GitHub release directly using the
[Create Release workflow](.github/workflows/create-release.yaml) via
`workflow_dispatch`. When triggered manually, you must provide:

- **version**: Release version (e.g., `v0.0.1` or `v0.0.1-rc.0`)
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
- [Create Release Branch Workflow](.github/workflows/create-release-branch.yaml)
- [Konflux Operator Tag PipelineRun](.tekton/konflux-operator-tag.yaml)
- [Create Release Workflow](.github/workflows/create-release.yaml)
- [Notify and Trigger GitHub Release Pipeline](pipelines/notify-and-trigger-github-release/notify-and-trigger-github-release.yaml)
- [Send GitHub Release Event Task](tasks/send-github-release-event/send-github-release-event.yaml)
- [Community Operator PR Workflow](.github/workflows/community-operator-pr.yaml)
- [Create Community Operator PR Script](.github/scripts/create-community-operator-pr.sh)
- [Operator Pipelines Documentation](https://redhat-openshift-ecosystem.github.io/operator-pipelines/) (external)
- [Konflux Operator in Community Catalog](https://github.com/redhat-openshift-ecosystem/community-operators-prod/tree/main/operators/konflux) (external)
