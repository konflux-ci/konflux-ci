#!/bin/bash
set -euo pipefail

# Create Release Script
# This script creates a GitHub release using the GitHub CLI
#
# Usage:
#   create-release.sh <version> <commit_sha> <notes_file> <artifact_file> <draft> <generate_notes>
#
# Arguments:
#   version        - Release version (e.g., v0.2025.01)
#   commit_sha     - Full commit SHA to release
#   notes_file     - Path to release notes markdown file
#   artifact_file  - Path to artifact file to upload
#   draft          - "true" to create as draft, "false" otherwise
#   generate_notes - "true" to auto-generate commit history, "false" otherwise
#
# Example:
#   create-release.sh v0.2025.01 c5683934bbdf40fc5517d9cf491b381c4a2f049d /tmp/release-notes.md operator/dist/install.yaml true false

if [ $# -ne 6 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <version> <commit_sha> <notes_file> <artifact_file> <draft> <generate_notes>"
  echo "  draft and generate_notes should be 'true' or 'false'"
  exit 1
fi

VERSION="$1"
COMMIT_SHA="$2"
NOTES_FILE="$3"
ARTIFACT_FILE="$4"
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

if [ ! -f "$ARTIFACT_FILE" ]; then
  echo "Error: Artifact file not found: $ARTIFACT_FILE"
  exit 1
fi

# Create release using official GitHub CLI (gh)
# Uses GITHUB_TOKEN (if permissions are insufficient, we can add GitHub App token later)
gh release create "$VERSION" \
  --title "Release $VERSION" \
  --notes-file "$NOTES_FILE" \
  $GENERATE_NOTES_FLAG \
  $DRAFT_FLAG \
  "$ARTIFACT_FILE" \
  --target "$COMMIT_SHA"

echo "Release created successfully: $VERSION"
