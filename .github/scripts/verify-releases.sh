#!/bin/bash
set -euo pipefail

# Verify Releases Script
# This script checks if a release was created in the past 7 days and if commits were made
#
# Usage:
#   verify-releases.sh <repository>
#
# Arguments:
#   repository - Repository in format owner/repo (required)
#
# Environment Variables:
#   GH_TOKEN      - GitHub token for API access (required)
#   GITHUB_ENV    - Path to GitHub Actions environment file (automatically set by GitHub Actions)
#
# The script sets the following environment variables (via GITHUB_ENV):
#   COMMIT_COUNT         - Number of commits found in the past 7 days
#   VERIFICATION_FAILED  - "true" if verification failed (no release but commits exist), unset otherwise
#
# Exit Codes:
#   0 - Success (release exists OR no commits)
#   1 - Unexpected failure: Script error (should create generic issue)
#   2 - Expected failure: No release found but commits exist (should create verification issue)
#
# Example:
#   export GH_TOKEN="your_token"
#   verify-releases.sh owner/repo

if [ $# -lt 1 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <repository>"
  echo "  repository - Repository in format owner/repo"
  exit 1
fi

REPOSITORY="$1"

# Verify GH_TOKEN is set
if [ -z "${GH_TOKEN:-}" ]; then
  echo "Error: GH_TOKEN environment variable is not set"
  exit 1
fi

# Set up GitHub CLI authentication
export GH_TOKEN

# Initialize VERIFICATION_FAILED to false (will be set to true if verification fails)
echo "VERIFICATION_FAILED=false" >> "${GITHUB_ENV}"

echo "Checking for releases in the past 7 days..."

# Calculate the start time ONCE (7 days ago in seconds)
# This freezes the window so both commands (to get releases and commits) use the exact same instant
START_TIMESTAMP=$(date -u --date='7 days ago' +%s)

# Check for releases in the past 7 days using gh release list
# Use --arg to safely pass the bash variable into the jq query
# shellcheck disable=SC2016
# $start is a jq variable (passed via --arg), not a bash variable, so single quotes are correct
# Wrap in error handling to catch unexpected failures
if ! RELEASE_COUNT=$(gh release list --repo "$REPOSITORY" --limit 100 \
  --json publishedAt \
  --jq --arg start "$START_TIMESTAMP" 'map(select((.publishedAt | fromdate) > ($start | tonumber))) | length' 2>&1); then
  echo "Error: Failed to check releases - ${RELEASE_COUNT}"
  exit 1
fi

if [ "$RELEASE_COUNT" -gt 0 ]; then
  echo "Found $RELEASE_COUNT release(s) in the past 7 days"
  RELEASE_EXISTS="true"
else
  echo "No releases found in the past 7 days"
  RELEASE_EXISTS="false"
fi

# Get commits in the past 7 days on main branch (excluding merge commits for cleaner output)
# Git accepts the @timestamp syntax for absolute time
COMMITS=$(git log --since="@$START_TIMESTAMP" \
  --oneline --no-merges main 2>/dev/null || echo "")

# Count commits
COMMIT_COUNT=$(echo "$COMMITS" | wc -l | tr -d ' ')

if [ "$COMMIT_COUNT" -gt 0 ]; then
  echo ""
  echo "Found $COMMIT_COUNT commit(s) in the past 7 days on main branch"
  COMMITS_EXIST="true"

  # Save commits to a file for the issue creation
  echo "$COMMITS" > /tmp/recent-commits.txt

  # Show all commits in workflow logs
  echo ""
  echo "Recent commits:"
  echo "$COMMITS"
else
  echo ""
  echo "No commits found in the past 7 days on main branch"
  COMMITS_EXIST="false"
  COMMIT_COUNT=0
fi

# Set environment variable for use in workflow (persist across steps)
echo "COMMIT_COUNT=${COMMIT_COUNT}" >> "${GITHUB_ENV}"

# Fail if no release exists but commits were made (expected failure - exit code 2)
if [ "$RELEASE_EXISTS" = "false" ] && [ "$COMMITS_EXIST" = "true" ]; then
  echo ""
  echo "❌ No release found in the past 7 days, but commits exist"
  echo "Commit count: ${COMMIT_COUNT}"
  echo ""
  echo "This workflow verifies that a release was created when commits are made to the main branch."
  echo "Please create a release for the recent commits."
  
  # Set flag to indicate this is an expected verification failure (should create issue)
  echo "VERIFICATION_FAILED=true" >> "${GITHUB_ENV}"
  exit 2
fi
