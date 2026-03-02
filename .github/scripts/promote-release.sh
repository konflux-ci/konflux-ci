#!/bin/bash
set -euo pipefail

# Promote Release Script
# Given a release candidate tag (vX.Y.Z-rc.W), creates the release tag (vX.Y.Z)
# pointing to the same commit. Fails if the release tag already exists (e.g. another
# RC was already promoted), so that we fail early with a clear message instead of
# relying on git rejecting the push.
#
# Usage:
#   promote-release.sh <release-candidate-tag>
#
# Arguments:
#   release-candidate-tag - RC tag to promote, format vX.Y.Z-rc.W (e.g. v1.2.3-rc.2)
#
# Environment:
#   GH_TOKEN - GitHub token with contents:write permission (required).
#
# Example:
#   export GH_TOKEN="your_token"
#   promote-release.sh v1.2.3-rc.2
#
# This script should be run from the repository root directory.

if [ $# -ne 1 ]; then
  echo "Usage: $0 <release-candidate-tag>"
  echo "Example: $0 v1.2.3-rc.2"
  exit 1
fi

RC_TAG="$1"

# Verify GH_TOKEN is set
if [ -z "${GH_TOKEN:-}" ]; then
  echo "Error: GH_TOKEN environment variable is not set"
  exit 1
fi

# Validate RC tag format: vX.Y.Z-rc.W
if [[ ! "$RC_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$ ]]; then
  echo "Error: Release candidate tag must match format vX.Y.Z-rc.W (e.g. v1.2.3-rc.2)"
  echo "Got: ${RC_TAG}"
  exit 1
fi

# Derive release tag by stripping -rc.W
RELEASE_TAG="${RC_TAG%-rc.*}"

echo "Promoting ${RC_TAG} -> ${RELEASE_TAG}"

# Ensure we have the RC tag fetched from origin
git fetch origin tag "${RC_TAG}" --no-recurse-submodules 2>/dev/null || true

# Resolve RC tag to commit; fail if tag does not exist
RC_SHA=$(git rev-parse "${RC_TAG}^{commit}" 2>/dev/null) || {
  echo "Error: Release candidate tag '${RC_TAG}' not found on origin. Ensure the tag exists."
  exit 1
}

# Check if the release tag already exists on origin
if git ls-remote --tags origin "refs/tags/${RELEASE_TAG}" | grep -q .; then
  git fetch origin tag "${RELEASE_TAG}" --no-recurse-submodules
  EXISTING_SHA=$(git rev-parse "${RELEASE_TAG}^{commit}")
  if [ "${EXISTING_SHA}" = "${RC_SHA}" ]; then
    echo "Release tag ${RELEASE_TAG} already exists on origin and points to the same commit as ${RC_TAG} (already promoted)."
    exit 0
  else
    echo "Error: Release tag ${RELEASE_TAG} already exists on origin and points to a different commit."
    echo "  ${RELEASE_TAG} -> ${EXISTING_SHA}"
    echo "  ${RC_TAG}      -> ${RC_SHA}"
    echo "Another release candidate may have been promoted already. Git would reject pushing the same tag to a different commit."
    exit 1
  fi
fi

# Create annotated release tag at the same commit as the RC tag
if ! git tag -a "${RELEASE_TAG}" "${RC_SHA}" -m "Promote ${RC_TAG} to ${RELEASE_TAG}"; then
  echo "Error: Failed to create tag ${RELEASE_TAG}"
  exit 1
fi

# Push the tag to origin, fail the script if it fails and clean up the local tag to avoid leaving a half-promoted tag.
echo "Pushing tag ${RELEASE_TAG} to origin..."
if ! git push origin "${RELEASE_TAG}" 2>&1; then
  echo "Error: Failed to push tag ${RELEASE_TAG} to origin" >&2
  echo "Cleaning up local tag..."
  git tag -d "${RELEASE_TAG}" 2>/dev/null || true
  exit 1
fi

echo "Successfully promoted ${RC_TAG} to ${RELEASE_TAG} (both point to ${RC_SHA})."
