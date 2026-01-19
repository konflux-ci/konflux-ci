#!/bin/bash
set -euo pipefail

# Auto Tag Main Script
# This script automatically tags the top of the main branch with an incremented patch version
#
# Usage:
#   auto-tag-main.sh
#
# This script should be run from the repository root directory.
#
# Example:
#   auto-tag-main.sh

# Configure git to use token for authentication (if GH_TOKEN is set)
if [ -n "${GH_TOKEN:-}" ]; then
  git config --local credential.helper store
  echo "https://x-access-token:${GH_TOKEN}@github.com" > ~/.git-credentials
fi

# Check if HEAD is already tagged
if git describe --tags --exact-match HEAD >/dev/null 2>&1; then
  EXISTING_TAG=$(git describe --tags --exact-match HEAD)
  echo "HEAD is already tagged with: ${EXISTING_TAG}"
  echo "Skipping tag creation as HEAD is already tagged."
  exit 0
fi

# Get the latest tag reachable from HEAD
LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")

if [ -z "$LATEST_TAG" ]; then
  echo "Error: No tags found in repository"
  echo "Please create an initial tag manually (e.g., v0.0.0)"
  exit 1
fi

# Validate that the tag matches semantic versioning pattern (vX.Y.Z)
if [[ ! "$LATEST_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: Latest tag '${LATEST_TAG}' does not match semantic versioning format (expected vX.Y.Z)"
  echo "Please ensure tags follow the format vX.Y.Z (e.g., v0.0.0)"
  exit 1
fi

echo "Latest tag on main: ${LATEST_TAG}"

# Extract version numbers (remove 'v' prefix)
VERSION="${LATEST_TAG#v}"

# Parse version components
IFS='.' read -r -a VERSION_PARTS <<< "$VERSION"
if [ ${#VERSION_PARTS[@]} -ne 3 ]; then
  echo "Error: Invalid version format: ${LATEST_TAG} (expected vX.Y.Z)"
  exit 1
fi

MAJOR="${VERSION_PARTS[0]}"
MINOR="${VERSION_PARTS[1]}"
PATCH="${VERSION_PARTS[2]}"

# Increment patch version (z number)
NEW_PATCH=$((PATCH + 1))
NEW_VERSION="v${MAJOR}.${MINOR}.${NEW_PATCH}"

echo "Incrementing patch version: ${LATEST_TAG} -> ${NEW_VERSION}"

# Create and push the tag
echo "Creating tag ${NEW_VERSION} on HEAD..."
git tag -a "${NEW_VERSION}" -m "Auto-tagged weekly release: ${NEW_VERSION}"

echo "Pushing tag ${NEW_VERSION} to origin..."
git push origin "${NEW_VERSION}"

echo "Successfully created and pushed tag: ${NEW_VERSION}"
