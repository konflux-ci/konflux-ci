#!/bin/bash
set -euo pipefail

# Create Community Operator PR Script
# This script creates a PR to the community-operators-prod repository
# with a new bundle version for the Konflux operator.
#
# Usage:
#   create-community-operator-pr.sh <release_tag>
#
# Arguments:
#   release_tag - GitHub release tag (e.g., v0.0.4)
#
# Environment Variables (required):
#   GITHUB_TOKEN - PAT with public_repo scope for all operations
#
# Example:
#   GITHUB_TOKEN=ghp_xxx create-community-operator-pr.sh v0.0.4

# Configuration
UPSTREAM_REPO="redhat-openshift-ecosystem/community-operators-prod"
FORK_REPO="konflux-ci/community-operators-prod"
SOURCE_REPO="konflux-ci/konflux-ci"
OPERATOR_NAME="konflux"

if [ $# -ne 1 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <release_tag>"
  exit 1
fi

RELEASE_TAG="$1"

if [ -z "${GITHUB_TOKEN:-}" ]; then
  echo "Error: GITHUB_TOKEN environment variable is required"
  exit 1
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
find "${OPERATOR_DIR}" -type f | head -20

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

# Create PR to upstream
echo ""
echo "=== Creating Pull Request ==="

# Create PR to upstream repository
PR_URL=$(GH_TOKEN="${GITHUB_TOKEN}" gh pr create \
  --repo "${UPSTREAM_REPO}" \
  --head "${FORK_REPO%%/*}:${BRANCH_NAME}" \
  --base main \
  --title "${COMMIT_TITLE}" \
  --body "${COMMIT_BODY}")

echo ""
echo "=== PR Created Successfully ==="
echo "PR URL: ${PR_URL}"
echo ""
echo "Done!"
