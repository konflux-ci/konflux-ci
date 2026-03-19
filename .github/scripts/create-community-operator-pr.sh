#!/bin/bash
set -euo pipefail

# Create or update Community Operator PR Script
# Creates a PR to the community-operators-prod repository with a new bundle
# version for the Konflux operator, or updates an existing PR branch (rebase
# on current main) and marks it ready for review.
#
# Usage:
#   create-community-operator-pr.sh <release_tag> [--draft|--no-draft]
#   create-community-operator-pr.sh <release_tag> --update
#
# Arguments:
#   release_tag - GitHub release tag (e.g., v0.0.4)
#   --draft     - Create PR as draft (default when creating)
#   --no-draft  - Create PR ready for review
#   --update    - Update mode: re-run same steps (clone main, apply bundle, force-push),
#                 then find the PR by head branch and mark it ready.
#   --dry-run   - Validate args and print branch name; no gh/git operations. No token needed.
#
# Environment Variables (required unless --dry-run):
#   GITHUB_TOKEN - PAT with public_repo scope (same identity as PR author for --update)
#
# Examples:
#   GITHUB_TOKEN=ghp_xxx create-community-operator-pr.sh v0.0.4 --draft
#   GITHUB_TOKEN=ghp_xxx create-community-operator-pr.sh v0.0.4 --update

# Configuration
UPSTREAM_REPO="redhat-openshift-ecosystem/community-operators-prod"
FORK_REPO="konflux-ci/community-operators-prod"
SOURCE_REPO="konflux-ci/konflux-ci"
OPERATOR_NAME="konflux"

RELEASE_TAG=""
CREATE_DRAFT="true"
UPDATE_MODE="false"
DRY_RUN="false"

while [ $# -gt 0 ]; do
  case "$1" in
    --draft)
      CREATE_DRAFT="true"
      shift
      ;;
    --no-draft)
      CREATE_DRAFT="false"
      shift
      ;;
    --update)
      UPDATE_MODE="true"
      CREATE_DRAFT="false"
      shift
      ;;
    --dry-run)
      DRY_RUN="true"
      shift
      ;;
    -*)
      echo "Error: Unknown option $1"
      exit 1
      ;;
    *)
      if [ -z "${RELEASE_TAG}" ]; then
        RELEASE_TAG="$1"
      else
        echo "Error: Unexpected argument $1"
        exit 1
      fi
      shift
      ;;
  esac
done

if [ -z "${RELEASE_TAG}" ]; then
  echo "Error: release_tag is required"
  echo "Usage: $0 <release_tag> [--draft|--no-draft] [--update] [--dry-run]"
  exit 1
fi

if [ "${DRY_RUN}" != "true" ] && [ -z "${GITHUB_TOKEN:-}" ]; then
  echo "Error: GITHUB_TOKEN environment variable is required"
  exit 1
fi

# Dry-run: validate and print what would be done; no network or git operations
if [ "${DRY_RUN}" = "true" ]; then
  VERSION="${RELEASE_TAG#v}"
  BRANCH_NAME="${OPERATOR_NAME}-${VERSION}"
  echo "=== Dry run ==="
  echo "Release tag: ${RELEASE_TAG}"
  echo "Version: ${VERSION}"
  echo "Branch name: ${BRANCH_NAME}"
  echo "Mode: $([ "${UPDATE_MODE}" = "true" ] && echo "update (refresh branch, mark PR ready)" || echo "create (draft=${CREATE_DRAFT})")"
  echo "Would use: SOURCE_REPO=${SOURCE_REPO} FORK_REPO=${FORK_REPO} UPSTREAM_REPO=${UPSTREAM_REPO}"
  exit 0
fi

echo "=== Creating Community Operator PR ==="
echo "Release tag: ${RELEASE_TAG}"
echo "Source repo: ${SOURCE_REPO}"
echo "Fork repo: ${FORK_REPO}"
echo "Upstream repo: ${UPSTREAM_REPO}"

# Create temporary directory for work
WORK_DIR=$(mktemp -d)
trap 'rm -rf "${WORK_DIR}"' EXIT
echo "Working directory: ${WORK_DIR}"

# Download release assets
echo ""
echo "=== Downloading release assets ==="
ASSETS_DIR="${WORK_DIR}/assets"
mkdir -p "${ASSETS_DIR}"

# Download version file (use GITHUB_TOKEN for source repo access)
echo "Downloading version file..."
GH_TOKEN="${GITHUB_TOKEN}" gh release download "${RELEASE_TAG}" \
  --repo "${SOURCE_REPO}" \
  --pattern "version" \
  --dir "${ASSETS_DIR}"

# Download bundle tarball
echo "Downloading bundle.tar.gz..."
GH_TOKEN="${GITHUB_TOKEN}" gh release download "${RELEASE_TAG}" \
  --repo "${SOURCE_REPO}" \
  --pattern "bundle.tar.gz" \
  --dir "${ASSETS_DIR}"

# Read and process version
VERSION_WITH_V=$(cat "${ASSETS_DIR}/version")
VERSION="${VERSION_WITH_V#v}"  # Strip leading 'v'
echo "Version (with v): ${VERSION_WITH_V}"
echo "Version (without v): ${VERSION}"

# Extract bundle
echo ""
echo "=== Extracting bundle ==="
BUNDLE_DIR="${WORK_DIR}/bundle"
mkdir -p "${BUNDLE_DIR}"
tar xzf "${ASSETS_DIR}/bundle.tar.gz" -C "${BUNDLE_DIR}"
echo "Bundle contents:"
ls -la "${BUNDLE_DIR}"

# Clone with treeless filter + sparse checkout (fastest - no tree/blob download until needed)
echo ""
echo "=== Cloning repository (treeless + sparse) ==="
REPO_DIR="${WORK_DIR}/community-operators-prod"

# Use treeless clone with sparse checkout - only downloads what's actually checked out
git clone \
  --filter=tree:0 \
  --no-checkout \
  --depth 1 \
  --single-branch \
  --branch main \
  "https://github.com/${UPSTREAM_REPO}.git" \
  "${REPO_DIR}"

cd "${REPO_DIR}"

# Configure git
git config user.name "konflux-ci-bot"
git config user.email "konflux-ci-maintainers@redhat.com"

# Add fork as origin for pushing
git remote set-url origin "https://x-access-token:${GITHUB_TOKEN}@github.com/${FORK_REPO}.git"

# Enable sparse checkout - only checkout the operator directory we need
git sparse-checkout init --cone
git sparse-checkout set "operators/${OPERATOR_NAME}"
git checkout

# Create branch for our changes
BRANCH_NAME="${OPERATOR_NAME}-${VERSION}"
echo ""
echo "=== Creating branch: ${BRANCH_NAME} ==="
git checkout -b "${BRANCH_NAME}"

# Create operator version directory
OPERATOR_DIR="operators/${OPERATOR_NAME}/${VERSION}"
echo ""
echo "=== Creating operator directory: ${OPERATOR_DIR} ==="
mkdir -p "${OPERATOR_DIR}"

# Copy all bundle contents (manifests, metadata, tests, Dockerfile, release-config.yaml, etc.)
cp -r "${BUNDLE_DIR}"/* "${OPERATOR_DIR}/"
echo "✅ Copied all bundle contents"

echo ""
echo "Operator directory contents:"
find "${OPERATOR_DIR}" -type f | head -20 || true

# Prepare commit/PR message
COMMIT_TITLE="operator ${OPERATOR_NAME} (${VERSION})"
COMMIT_BODY="## New Operator Bundle

**Operator:** ${OPERATOR_NAME}
**Version:** ${VERSION}
**Source Release:** https://github.com/${SOURCE_REPO}/releases/tag/${RELEASE_TAG}

This PR adds a new bundle version for the Konflux operator.

### Changes
- Added bundle for version ${VERSION}
"

# Stage and commit changes
echo ""
echo "=== Committing changes ==="
git add "${OPERATOR_DIR}"
git commit -s -m "${COMMIT_TITLE}" -m "${COMMIT_BODY}"
echo "Commit title: ${COMMIT_TITLE}"

# Push to fork
echo ""
echo "=== Pushing to fork ==="
git push --force origin "${BRANCH_NAME}"

# Create PR or mark existing PR ready
echo ""
if [ "${UPDATE_MODE}" = "true" ]; then
  PR_NUM=$(GH_TOKEN="${GITHUB_TOKEN}" gh pr list --repo "${UPSTREAM_REPO}" \
    --author "@me" --head "${BRANCH_NAME}" --state open --json number -q '.[0].number')
  if [ -z "${PR_NUM}" ] || [ "${PR_NUM}" = "null" ]; then
    echo "Error: No open PR found for branch ${BRANCH_NAME}"
    exit 1
  fi
  echo "=== Marking PR #${PR_NUM} ready for review ==="
  GH_TOKEN="${GITHUB_TOKEN}" gh pr ready "${PR_NUM}" --repo "${UPSTREAM_REPO}"
  PR_URL="https://github.com/${UPSTREAM_REPO}/pull/${PR_NUM}"
  echo ""
  echo "=== PR updated and marked ready ==="
  echo "PR URL: ${PR_URL}"
else
  echo "=== Creating Pull Request ==="
  DRAFT_ARGS=""
  if [ "${CREATE_DRAFT}" = "true" ]; then
    DRAFT_ARGS="--draft"
  fi
  PR_URL=$(GH_TOKEN="${GITHUB_TOKEN}" gh pr create \
    ${DRAFT_ARGS} \
    --repo "${UPSTREAM_REPO}" \
    --head "${FORK_REPO%%/*}:${BRANCH_NAME}" \
    --base main \
    --title "${COMMIT_TITLE}" \
    --body "${COMMIT_BODY}")
  echo ""
  echo "=== PR Created Successfully ==="
  echo "PR URL: ${PR_URL}"
fi
echo ""
echo "Done!"
