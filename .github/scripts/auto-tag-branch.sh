#!/bin/bash
set -euo pipefail

# Auto Tag Branch Script
# Tags the current branch with branch-aware versioning for LTS support:
#   - main: vX.Y.Z-rc.W (find latest rc tag on main, increment W)
#   - release-x.y: vX.Y.Z (find latest vX.Y.Z on branch, increment Z)
# In both cases we only consider tags reachable from HEAD and increment the number after the rightmost dot.
#
# Usage:
#   BRANCH=main .github/scripts/auto-tag-branch.sh
#   BRANCH=release-1.2 .github/scripts/auto-tag-branch.sh
#
# This script should be run from the repository root directory.

BRANCH="${BRANCH:-main}"

# Configure git to use token for authentication (if GH_TOKEN is set)
if [ -n "${GH_TOKEN:-}" ]; then
  git config --local credential.helper store
  echo "https://x-access-token:${GH_TOKEN}@github.com" > ~/.git-credentials
fi

# Branch-aware versioning: release-x.y -> vX.Y.Z, increment Z; main -> vX.Y.Z-rc.W, increment W
# The same commit can be on both main and release-x.y and get two tags (e.g. v1.3.0-rc.1 and v1.2.0).
# So we only skip if HEAD already has a tag in *this branch's* format.
if [[ "$BRANCH" =~ ^release-([0-9]+)\.([0-9]+)$ ]]; then
  # Release branch: only consider tags vX.Y.Z (no -rc) reachable from HEAD for this branch
  MAJOR="${BASH_REMATCH[1]}"
  MINOR="${BASH_REMATCH[2]}"
  PREFIX="v${MAJOR}.${MINOR}."
  # Skip only if HEAD already has a tag in this branch's format (vX.Y.Z for this line)
  EXISTING=$(git tag --points-at HEAD 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | grep -E "^${PREFIX}" || true)
  if [ -n "$EXISTING" ]; then
    echo "HEAD is already tagged for ${BRANCH} with: $(echo "$EXISTING" | head -1)"
    echo "Skipping tag creation for this branch."
    exit 0
  fi
  # List tags merged in HEAD matching vX.Y.*, keep only vX.Y.Z (three numeric parts, no suffix)
  LATEST_TAG=$(git tag --merged=HEAD -l "${PREFIX}*" 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)

  if [ -z "$LATEST_TAG" ]; then
    echo "Error: No tags matching v${MAJOR}.${MINOR}.Z found reachable from HEAD on branch ${BRANCH}"
    echo "Create an initial tag for this line (e.g., v${MAJOR}.${MINOR}.0) on this branch."
    exit 1
  fi

  VERSION="${LATEST_TAG#v}"
  IFS='.' read -r -a VERSION_PARTS <<< "$VERSION"
  [ ${#VERSION_PARTS[@]} -eq 3 ] || { echo "Error: Invalid version format: ${LATEST_TAG}"; exit 1; }
  PATCH="${VERSION_PARTS[2]}"
  NEW_PATCH=$((PATCH + 1))
  NEW_VERSION="v${MAJOR}.${MINOR}.${NEW_PATCH}"
  echo "Incrementing patch on ${BRANCH}: ${LATEST_TAG} -> ${NEW_VERSION}"

else
  # main: vX.Y.Z-rc.W — find latest rc tag on main (reachable from HEAD), increment W
  RC_TAG_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$'
  # Skip only if HEAD already has a tag in this branch's format (rc tag)
  EXISTING=$(git tag --points-at HEAD 2>/dev/null | grep -E "$RC_TAG_PATTERN" || true)
  if [ -n "$EXISTING" ]; then
    echo "HEAD is already tagged for main with: $(echo "$EXISTING" | head -1)"
    echo "Skipping tag creation for this branch."
    exit 0
  fi
  LATEST_RC=$(git tag --merged=HEAD 2>/dev/null | grep -E "$RC_TAG_PATTERN" | sort -V | tail -1 || true)

  if [ -n "$LATEST_RC" ]; then
    # Parse vX.Y.Z-rc.W and increment W
    if [[ "$LATEST_RC" =~ ^(v[0-9]+\.[0-9]+\.[0-9]+)-rc\.([0-9]+)$ ]]; then
      BASE="${BASH_REMATCH[1]}"
      W="${BASH_REMATCH[2]}"
      NEW_W=$((W + 1))
      NEW_VERSION="${BASE}-rc.${NEW_W}"
      echo "Latest rc tag on ${BRANCH}: ${LATEST_RC}"
      echo "Incrementing rc number on main: ${LATEST_RC} -> ${NEW_VERSION}"
    else
      echo "Error: Could not parse rc tag: ${LATEST_RC}"
      exit 1
    fi
  else
    # No rc tag on main yet: use next minor line, first rc (e.g. latest v1.2.5 -> v1.3.0-rc.1)
    LATEST_TAG=$(git tag --merged=HEAD 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)
    if [ -z "$LATEST_TAG" ]; then
      echo "Error: No tags found on main (need at least one vX.Y.Z or vX.Y.Z-rc.W)"
      echo "Please create an initial tag manually (e.g., v0.0.0 or v1.0.0-rc.1)"
      exit 1
    fi
    VERSION="${LATEST_TAG#v}"
    IFS='.' read -r -a VERSION_PARTS <<< "$VERSION"
    [ ${#VERSION_PARTS[@]} -eq 3 ] || { echo "Error: Invalid version format: ${LATEST_TAG}"; exit 1; }
    MAJOR="${VERSION_PARTS[0]}"
    MINOR="${VERSION_PARTS[1]}"
    NEW_MINOR=$((MINOR + 1))
    NEW_VERSION="v${MAJOR}.${NEW_MINOR}.0-rc.1"
    echo "No rc tag on main yet; latest stable: ${LATEST_TAG}"
    echo "Creating first rc for next minor: ${NEW_VERSION}"
  fi
fi

echo "Creating tag ${NEW_VERSION} on HEAD..."
git tag -a "${NEW_VERSION}" -m "Auto-tagged weekly release: ${NEW_VERSION}"

echo "Pushing tag ${NEW_VERSION} to origin..."
git push origin "${NEW_VERSION}"

echo "Successfully created and pushed tag: ${NEW_VERSION}"
