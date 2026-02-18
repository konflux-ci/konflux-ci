#!/bin/bash
set -euo pipefail

# Create Release Branch
# 1. Creates the release branch at git_ref (no tag on the branch; periodic workflow will tag it).
# 2. Updates .tekton files for per-release resources (application, component, branch, tag trigger).
# 3. Creates a PR to main that adds the dev version to the on-tag pipeline trigger.
# 4. Tags main with v{dev_version}.0-rc.0.
#
# Usage:
#   create-release-branch-and-pr.sh <dev_version> <release_version> <git_ref> <remote_name>
#
# Arguments:
#   dev_version     - Next dev line X.Y (e.g. 1.2 → tag v1.2.0-rc.0 on main)
#   release_version - Release line X.Y (e.g. 1.1 → branch release-1.1, tags v1.1.*)
#   git_ref         - Ref to create branch from (e.g. main, or a commit SHA).
#   remote_name     - Git remote to push to (e.g. origin).
#
# Environment:
#   GH_TOKEN         - Used for push and gh pr (required in CI).
#   REPO_ROOT        - Optional. Defaults to GITHUB_WORKSPACE or git rev-parse --show-toplevel.

REPO_ROOT="${REPO_ROOT:-${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}}"

if [ $# -ne 4 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <dev_version> <release_version> <git_ref> <remote_name>"
  exit 1
fi

DEV_VERSION="$1"
RELEASE_VERSION="$2"
GIT_REF="$3"
REMOTE_NAME="$4"

if [ -z "${GIT_REF}" ] || [ -z "${REMOTE_NAME}" ]; then
  echo "Error: git_ref and remote_name must be non-empty"
  exit 1
fi

if [[ ! "$DEV_VERSION" =~ ^[0-9]+\.[0-9]+$ ]] || [[ ! "$RELEASE_VERSION" =~ ^[0-9]+\.[0-9]+$ ]]; then
  echo "Error: dev_version and release_version must be X.Y (e.g. 1.2)"
  exit 1
fi

RELEASE_BRANCH="release-${RELEASE_VERSION}"
TAG_DEV_RC="v${DEV_VERSION}.0-rc.0"
APP_COMPONENT="konflux-operator-${RELEASE_VERSION/\./-}"
TEKTON_DIR="${REPO_ROOT}/.tekton"
TAG_PIPELINE="${TEKTON_DIR}/konflux-operator-tag.yaml"
MAIN_TAG_TRIGGER_BRANCH="add-dev-version-tag-trigger-${DEV_VERSION}"

cd "$REPO_ROOT"

if [ "${GITHUB_ACTIONS:-}" = "true" ] && [ -n "${GH_TOKEN:-}" ]; then
  git config --local user.email "github-actions[bot]@users.noreply.github.com"
  git config --local user.name "github-actions[bot]"
  git config --local credential.helper store
  echo "https://x-access-token:${GH_TOKEN}@github.com" > ~/.git-credentials
fi

TARGET_SHA=$(git rev-parse HEAD)
echo "Target commit: ${TARGET_SHA}"

if git ls-remote --heads "${REMOTE_NAME}" "${RELEASE_BRANCH}" | grep -q .; then
  echo "Error: Branch ${RELEASE_BRANCH} already exists on ${REMOTE_NAME}."
  exit 1
fi

# --- 1. Create release branch at git_ref ---
git checkout -b "${RELEASE_BRANCH}"
echo "Created branch ${RELEASE_BRANCH}"

# --- 2. Update .tekton for per-release resources (using yq) ---
if ! command -v yq &>/dev/null; then
  echo "Error: yq is required but not installed."
  exit 1
fi

echo "Updating pipelineRuns on branch ${RELEASE_BRANCH}..."

RELEASE_ESC="${RELEASE_VERSION//./\\.}"
# CEL strings for yq env(); exported for child process only (not re-interpreted by shell) - shellcheck SC2089/SC2090 false positive
# shellcheck disable=SC2089,SC2090
TAG_CEL='event == "push" && target_branch.matches("refs/tags/v'"${RELEASE_ESC}"'\\.*")'
# shellcheck disable=SC2089
PUSH_CEL='event == "push" && target_branch == "'"${RELEASE_BRANCH}"'"'
# shellcheck disable=SC2089
PR_CEL='event == "pull_request" && target_branch == "'"${RELEASE_BRANCH}"'" || event == "push" && target_branch.startsWith("gh-readonly-queue/'"${RELEASE_BRANCH}"'/")'

export APP_COMPONENT
for f in "${TEKTON_DIR}"/konflux-operator-push.yaml "${TEKTON_DIR}"/konflux-operator-pull-request.yaml "${TEKTON_DIR}"/konflux-operator-tag.yaml; do
  [ -f "$f" ] || continue
  yq -i '
    .metadata.labels["appstudio.openshift.io/application"] = env(APP_COMPONENT) |
    .metadata.labels["appstudio.openshift.io/component"] = env(APP_COMPONENT) |
    .spec.taskRunTemplate.serviceAccountName = "build-pipeline-" + env(APP_COMPONENT)
  ' "$f"
done

# shellcheck disable=SC2090
export PUSH_CEL
yq -i '.metadata.name = env(APP_COMPONENT) + "-on-push" | .metadata.annotations["pipelinesascode.tekton.dev/on-cel-expression"] = env(PUSH_CEL)' "${TEKTON_DIR}/konflux-operator-push.yaml"

# shellcheck disable=SC2090
export PR_CEL
yq -i '.metadata.name = env(APP_COMPONENT) + "-on-pull-request" | .metadata.annotations["pipelinesascode.tekton.dev/on-cel-expression"] = env(PR_CEL)' "${TEKTON_DIR}/konflux-operator-pull-request.yaml"

# shellcheck disable=SC2090
export TAG_CEL
yq -i '.metadata.name = env(APP_COMPONENT) + "-on-tag" | .metadata.annotations["pipelinesascode.tekton.dev/on-cel-expression"] = env(TAG_CEL)' "${TEKTON_DIR}/konflux-operator-tag.yaml"

echo "Pushing changes to branch ${RELEASE_BRANCH}..."

git add "${TEKTON_DIR}"
git commit -m "tekton: per-release resources for ${RELEASE_BRANCH}

- application/component: ${APP_COMPONENT}
- branch: ${RELEASE_BRANCH}
- tag trigger: v${RELEASE_VERSION}.*"

git push "${REMOTE_NAME}" "${RELEASE_BRANCH}"

# --- 4. PR to main: add dev version to on-tag pipeline trigger ---
echo "Updating pipelineRuns for main..."
git fetch "${REMOTE_NAME}" main
git checkout -b "${MAIN_TAG_TRIGGER_BRANCH}" "${REMOTE_NAME}/main"

DEV_ESC="${DEV_VERSION//./\\.}"
# shellcheck disable=SC2089,SC2090
NEW_CEL='event == "push" && target_branch.matches("refs/tags/v'"${DEV_ESC}"'\\.*")'
# shellcheck disable=SC2090
export NEW_CEL
yq -i '.metadata.annotations["pipelinesascode.tekton.dev/on-cel-expression"] = env(NEW_CEL)' "${TAG_PIPELINE}"

echo "Pushing changes and creating PR..."

git add "${TAG_PIPELINE}"
git commit -m "tekton: trigger on-tag pipeline for dev version v${DEV_VERSION}.*

- Add tag pattern refs/tags/v${DEV_VERSION}.* to konflux-operator-tag pipeline"
git push "${REMOTE_NAME}" "${MAIN_TAG_TRIGGER_BRANCH}" --force-with-lease

PR_TITLE="tekton: trigger on-tag pipeline for dev version v${DEV_VERSION}.*"
PR_BODY="Adds tag pattern \`refs/tags/v${DEV_VERSION}.*\` to the on-tag PipelineRun so that tags like \`v${TAG_DEV_RC}\` trigger the pipeline.

---
*Created by the [Create Release Branch workflow](.github/workflows/create-release-branch.yaml)*"
EXISTING_PR=$(gh pr view "${MAIN_TAG_TRIGGER_BRANCH}" --json number,state --jq 'if .number then "\(.number)|\(.state)" else empty end' 2>/dev/null || echo "")
if [ -n "${EXISTING_PR}" ]; then
  PR_NUM=$(echo "${EXISTING_PR}" | cut -d'|' -f1)
  [ "$(echo "${EXISTING_PR}" | cut -d'|' -f2)" = "OPEN" ] && gh pr edit "${PR_NUM}" --title "${PR_TITLE}" --body "${PR_BODY}"
else
  gh pr create --title "${PR_TITLE}" --body "${PR_BODY}" --base main --head "${MAIN_TAG_TRIGGER_BRANCH}"
fi

# --- 5. Tag main with v{dev_version}.0-rc.0 ---
git fetch "${REMOTE_NAME}" main
git checkout -B main "${REMOTE_NAME}/main"

if ! git rev-parse "${TAG_DEV_RC}" >/dev/null 2>&1; then
  git tag -a "${TAG_DEV_RC}" -m "Dev version ${TAG_DEV_RC} (rc.0)"
  git push "${REMOTE_NAME}" "${TAG_DEV_RC}"
  echo "Created and pushed tag ${TAG_DEV_RC} on main"
else
  echo "Tag ${TAG_DEV_RC} already exists; skipping."
fi

echo "Done."
