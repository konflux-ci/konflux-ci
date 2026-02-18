#!/bin/bash
set -euo pipefail

# Create Release Branch and Auto-Tag Matrix PR
# Creates a new release branch at a ref, tags it with dev rc and release tags,
# then opens a PR to add the branch to the auto-tag-weekly matrix.
#
# Usage:
#   create-release-branch-and-pr.sh <dev_version> <release_version> <git_ref> <remote_name>
#
# Arguments:
#   dev_version     - Next dev line X.Y (e.g. 1.2 -> tags v1.2.0-rc.1)
#   release_version - Release line X.Y (e.g. 1.1 -> branch release-1.1, tag v1.1.0)
#   git_ref         - Ref to create branch from (e.g. main, or a commit SHA).
#   remote_name     - Git remote to push to (e.g. origin).
#
# Environment:
#   GH_TOKEN         - Used for push and gh pr (required in CI).
#   REPO_ROOT        - Optional. Defaults to GITHUB_WORKSPACE or git rev-parse --show-toplevel.
#
# Example:
#   create-release-branch-and-pr.sh 1.2 1.1 main origin
#   create-release-branch-and-pr.sh 1.2 1.1 main upstream

REPO_ROOT="${REPO_ROOT:-${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}}"

if [ $# -ne 4 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <dev_version> <release_version> <git_ref> <remote_name>"
  echo "  dev_version     - X.Y (e.g. 1.2)"
  echo "  release_version - X.Y (e.g. 1.1)"
  echo "  git_ref         - Ref to create branch from (e.g. main)"
  echo "  remote_name     - Git remote to push to (e.g. origin)"
  exit 1
fi

DEV_VERSION="$1"
RELEASE_VERSION="$2"
GIT_REF="$3"
REMOTE_NAME="$4"

if [ -z "${GIT_REF}" ]; then
  echo "Error: git_ref must be non-empty"
  exit 1
fi
if [ -z "${REMOTE_NAME}" ]; then
  echo "Error: remote_name must be non-empty"
  exit 1
fi

# Validate X.Y format (one or more digits, dot, one or more digits)
if [[ ! "$DEV_VERSION" =~ ^[0-9]+\.[0-9]+$ ]]; then
  echo "Error: dev_version must be X.Y (e.g. 1.2), got: ${DEV_VERSION}"
  exit 1
fi
if [[ ! "$RELEASE_VERSION" =~ ^[0-9]+\.[0-9]+$ ]]; then
  echo "Error: release_version must be X.Y (e.g. 1.1), got: ${RELEASE_VERSION}"
  exit 1
fi

RELEASE_BRANCH="release-${RELEASE_VERSION}"
TAG_DEV="v${DEV_VERSION}.0-rc.1"
TAG_RELEASE="v${RELEASE_VERSION}.0"
WORKFLOW_FILE="${REPO_ROOT}/.github/workflows/auto-tag-weekly.yaml"
MATRIX_PR_BRANCH="add-release-branch-${RELEASE_VERSION}"

cd "$REPO_ROOT"

if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  git config --local user.email "github-actions[bot]@users.noreply.github.com"
  git config --local user.name "github-actions[bot]"
  if [ -n "${GH_TOKEN:-}" ]; then
    git config --local credential.helper store
    echo "https://x-access-token:${GH_TOKEN}@github.com" > ~/.git-credentials
  fi
fi

# Workflow checks out at git_ref; we work from HEAD for the whole script
TARGET_SHA=$(git rev-parse HEAD)
echo "Target commit: ${TARGET_SHA}"

# Fail if release branch already exists on remote
if git ls-remote --heads "${REMOTE_NAME}" "${RELEASE_BRANCH}" | grep -q .; then
  echo "Error: Branch ${RELEASE_BRANCH} already exists on ${REMOTE_NAME}."
  exit 1
fi

# Create release branch at target commit (we are at TARGET_SHA)
git checkout -b "${RELEASE_BRANCH}"

# Tag the commit (both tags on the same commit)
git tag -a "${TAG_DEV}" -m "Release branch creation: ${TAG_DEV}"
git tag -a "${TAG_RELEASE}" -m "Release ${TAG_RELEASE}"

echo "Created branch ${RELEASE_BRANCH} and tags ${TAG_DEV}, ${TAG_RELEASE}"

# Push branch and tags
git push "${REMOTE_NAME}" "${RELEASE_BRANCH}"
git push "${REMOTE_NAME}" "${TAG_DEV}" "${TAG_RELEASE}"

# --- Update auto-tag-weekly matrix and open PR (from same ref we started at) ---
if git show-ref --verify --quiet refs/heads/"${MATRIX_PR_BRANCH}"; then
  git branch -D "${MATRIX_PR_BRANCH}"
fi
if git ls-remote --heads "${REMOTE_NAME}" "${MATRIX_PR_BRANCH}" | grep -q .; then
  echo "Branch ${MATRIX_PR_BRANCH} already exists on ${REMOTE_NAME}; will update it."
  git fetch "${REMOTE_NAME}" "${MATRIX_PR_BRANCH}"
  git checkout -b "${MATRIX_PR_BRANCH}" "${REMOTE_NAME}/${MATRIX_PR_BRANCH}"
else
  git checkout -b "${MATRIX_PR_BRANCH}" "${TARGET_SHA}"
fi

# Add release branch to matrix if not already present (using yq)
if ! command -v yq &>/dev/null; then
  echo "Error: yq is required but not installed."
  exit 1
fi

CURRENT_LIST=$(yq '.jobs["auto-tag"].strategy.matrix.branch' "${WORKFLOW_FILE}" 2>/dev/null || true)
if echo "$CURRENT_LIST" | grep -q "${RELEASE_BRANCH}"; then
  echo "Branch ${RELEASE_BRANCH} is already in the matrix; no workflow change needed."
else
  # yq: add to array and deduplicate (mikefarah/yq v4 style)
  yq -i ".jobs[\"auto-tag\"].strategy.matrix.branch |= (. + [\"${RELEASE_BRANCH}\"] | unique)" "${WORKFLOW_FILE}"
  git add "${WORKFLOW_FILE}"
  git commit -m "ci: add ${RELEASE_BRANCH} to Auto Tag Weekly matrix

- Enables weekly patch tagging for ${RELEASE_BRANCH} (e.g. ${TAG_RELEASE} -> v${RELEASE_VERSION}.1)."

  git push "${REMOTE_NAME}" "${MATRIX_PR_BRANCH}" --force

  PR_TITLE="ci: add ${RELEASE_BRANCH} to Auto Tag Weekly matrix"
  PR_BODY="## Add release branch to weekly auto-tag

- **Release branch:** \`${RELEASE_BRANCH}\`
- **Tags created:** \`${TAG_DEV}\`, \`${TAG_RELEASE}\` at \`${TARGET_SHA}\`

This PR adds \`${RELEASE_BRANCH}\` to the matrix in [.github/workflows/auto-tag-weekly.yaml](.github/workflows/auto-tag-weekly.yaml) so the weekly job will create patch tags (e.g. v${RELEASE_VERSION}.1) for this branch.

---
*Created by the [Create Release Branch workflow](.github/workflows/create-release-branch.yaml)*"

  EXISTING_PR=$(gh pr view "${MATRIX_PR_BRANCH}" --json number,state --jq 'if .number then "\(.number)|\(.state)" else empty end' 2>/dev/null || echo "")
  if [ -n "${EXISTING_PR}" ]; then
    PR_NUM=$(echo "${EXISTING_PR}" | cut -d'|' -f1)
    PR_STATE=$(echo "${EXISTING_PR}" | cut -d'|' -f2)
    if [ "${PR_STATE}" = "OPEN" ]; then
      echo "Updated existing PR #${PR_NUM}"
      gh pr edit "${PR_NUM}" --title "${PR_TITLE}" --body "${PR_BODY}"
      git checkout main
      exit 0
    fi
  fi

  gh pr create \
    --title "${PR_TITLE}" \
    --body "${PR_BODY}" \
    --base main \
    --head "${MATRIX_PR_BRANCH}"
fi

git checkout main
echo "Done."
