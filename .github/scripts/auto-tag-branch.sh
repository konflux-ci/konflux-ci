#!/bin/bash
set -euo pipefail

# Auto Tag Branch Script
# Creates RC tags only. Run on release-x.y branches.
#   - If latest tag reachable from HEAD is vX.Y.Z (no -rc): create vX.Y.(Z+1)-rc.0
#   - If latest tag is vX.Y.Z-rc.W: create vX.Y.Z-rc.(W+1)
# Skips if the computed next tag already exists.
#
# Usage:
#   .github/scripts/auto-tag-branch.sh
#
# This script should be run from the repository root directory
# with the target branch already checked out.

# Configure git to use token for authentication (if GH_TOKEN is set)
if [ -n "${GH_TOKEN:-}" ]; then
  git config --local credential.helper store
  echo "https://x-access-token:${GH_TOKEN}@github.com" > ~/.git-credentials
fi

# Version tags: vX.Y.Z or vX.Y.Z-rc.W
STABLE_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+$'
VERSION_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$'

# Latest tag reachable from HEAD (any version tag, sort -V)
LATEST=$(git tag --merged=HEAD 2>/dev/null | grep -E "$VERSION_PATTERN" | sort -V | tail -1 || true)

if [ -z "$LATEST" ]; then
  echo "Error: No version tags (vX.Y.Z or vX.Y.Z-rc.W) reachable from HEAD."
  echo "Create an initial tag manually (e.g., v0.0.0 or v1.0.0-rc.0)."
  exit 1
fi

if [[ "$LATEST" =~ $STABLE_PATTERN ]]; then
  # vX.Y.Z → vX.Y.(Z+1)-rc.0
  VERSION="${LATEST#v}"
  IFS='.' read -r -a PARTS <<< "$VERSION"
  [ ${#PARTS[@]} -eq 3 ] || { echo "Error: Invalid version format: ${LATEST}"; exit 1; }
  X="${PARTS[0]}"
  Y="${PARTS[1]}"
  Z="${PARTS[2]}"
  NEW_VERSION="v${X}.${Y}.$((Z + 1))-rc.0"
  echo "Latest tag: ${LATEST} (stable)"
  echo "Creating next RC: ${NEW_VERSION}"

else
  # vX.Y.Z-rc.W → vX.Y.Z-rc.(W+1) (only other possibility after VERSION_PATTERN)
  [[ "$LATEST" =~ ^(v[0-9]+\.[0-9]+\.[0-9]+)-rc\.([0-9]+)$ ]]
  BASE="${BASH_REMATCH[1]}"
  W="${BASH_REMATCH[2]}"
  NEW_VERSION="${BASE}-rc.$((W + 1))"
  echo "Latest tag: ${LATEST} (rc)"
  echo "Incrementing RC: ${LATEST} -> ${NEW_VERSION}"
fi

if git rev-parse "$NEW_VERSION" &>/dev/null; then
  echo "Tag ${NEW_VERSION} already exists. Skipping."
  exit 0
fi

echo "Creating tag ${NEW_VERSION} on HEAD..."
git tag -a "${NEW_VERSION}" -m "Auto-tagged weekly release: ${NEW_VERSION}"

echo "Pushing tag ${NEW_VERSION} to origin..."
git push origin "${NEW_VERSION}"

echo "Successfully created and pushed tag: ${NEW_VERSION}"
