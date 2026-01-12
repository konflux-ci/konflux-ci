#!/bin/bash
set -euo pipefail

# Create Release Script
# This script creates a GitHub release using the GitHub CLI
#
# Usage:
#   create-release.sh <version> <git_ref> <notes_file> <artifact_dir> <draft> <generate_notes>
#
# Arguments:
#   version        - Release version (e.g., v0.2025.01)
#   git_ref        - Git ref to release (commit SHA, branch, or tag)
#   notes_file     - Path to release notes markdown file
#   artifact_dir   - Directory containing artifact files to upload
#   draft          - "true" to create as draft, "false" otherwise
#   generate_notes - "true" to auto-generate commit history, "false" otherwise
#
# Example:
#   create-release.sh v0.2025.01 c5683934bbdf40fc5517d9cf491b381c4a2f049d /tmp/release-notes.md operator/dist true false
#   create-release.sh v0.2025.01 main /tmp/release-notes.md operator/dist true false

if [ $# -ne 6 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <version> <git_ref> <notes_file> <artifact_dir> <draft> <generate_notes>"
  echo "  draft and generate_notes should be 'true' or 'false'"
  exit 1
fi

VERSION="$1"
GIT_REF="$2"
NOTES_FILE="$3"
ARTIFACT_DIR="$4"
DRAFT="$5"
GENERATE_NOTES="$6"

# Build flags based on boolean inputs
DRAFT_FLAG=""
if [ "$DRAFT" == "true" ]; then
  DRAFT_FLAG="--draft"
  echo "Creating draft release (no notifications will be sent)"
fi

GENERATE_NOTES_FLAG=""
if [ "$GENERATE_NOTES" == "true" ]; then
  GENERATE_NOTES_FLAG="--generate-notes"
  echo "Auto-generating commit history"
else
  echo "Skipping auto-generated commit history (use generate_notes=true for future releases)"
fi

# Verify files exist
if [ ! -f "$NOTES_FILE" ]; then
  echo "Error: Notes file not found: $NOTES_FILE"
  exit 1
fi

if [ ! -d "$ARTIFACT_DIR" ]; then
  echo "Error: Artifact directory not found: $ARTIFACT_DIR"
  exit 1
fi

# Collect all artifact files
ARTIFACTS=()
for file in "$ARTIFACT_DIR"/*; do
  if [ -f "$file" ]; then
    ARTIFACTS+=("$file")
    echo "Found artifact: $file"
  fi
done

if [ ${#ARTIFACTS[@]} -eq 0 ]; then
  echo "Error: No artifacts found in $ARTIFACT_DIR"
  exit 1
fi

# Create release using official GitHub CLI (gh)
# Uses GITHUB_TOKEN (if permissions are insufficient, we can add GitHub App token later)
gh release create "$VERSION" \
  --title "Release $VERSION" \
  --notes-file "$NOTES_FILE" \
  $GENERATE_NOTES_FLAG \
  $DRAFT_FLAG \
  "${ARTIFACTS[@]}" \
  --target "$GIT_REF"

echo "Release created successfully: $VERSION"
