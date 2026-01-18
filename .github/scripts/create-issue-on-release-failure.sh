#!/bin/bash
set -euo pipefail

# Create Issue on Release Failure Script
# This script creates or updates a GitHub issue when the release workflow fails
#
# Usage:
#   create-issue-on-release-failure.sh
#
# This script reads all parameters from environment variables set by the GitHub Actions workflow.
#
# Required Environment Variables:
#   GH_TOKEN                  - GitHub token with issues:write permission
#   GITHUB_REPOSITORY         - Repository in format owner/repo (or use GITHUB_REPOSITORY_OWNER + GITHUB_REPOSITORY_NAME)
#   GITHUB_REPOSITORY_OWNER   - Repository owner (from context.repo.owner)
#   GITHUB_RUN_ID             - Workflow run ID
#   GITHUB_EVENT_NAME         - Type of trigger (workflow_dispatch or repository_dispatch)
#
# Optional Environment Variables:
#   RELEASE_VERSION           - Release version (defaults to 'unknown')
#   RELEASE_GIT_REF           - Git ref to release (defaults to 'unknown')
#   RELEASE_IMAGE_TAG         - Image tag (defaults to 'unknown')
#   RELEASE_SOURCE            - Source of the release (defaults to GITHUB_EVENT_NAME)
#
# Note: Failure reason is automatically captured from the workflow run if available
#
# Example:
#   export GH_TOKEN="your_token"
#   export GITHUB_REPOSITORY="owner/repo"
#   export GITHUB_REPOSITORY_OWNER="owner"
#   export GITHUB_RUN_ID="123456789"
#   export GITHUB_EVENT_NAME="workflow_dispatch"
#   create-issue-on-release-failure.sh



# Extract release information from environment variables with defaults
VERSION="${RELEASE_VERSION:-unknown}"
GIT_REF="${RELEASE_GIT_REF:-unknown}"
IMAGE_TAG="${RELEASE_IMAGE_TAG:-unknown}"
SOURCE="${RELEASE_SOURCE:-${GITHUB_EVENT_NAME:-unknown}}"
RUN_ID="${GITHUB_RUN_ID:-unknown}"
TRIGGER_TYPE_RAW="${GITHUB_EVENT_NAME:-unknown}"
FAILURE_REASON=""

# Convert trigger type to meaningful name for better identification
if [ "$TRIGGER_TYPE_RAW" == "workflow_dispatch" ]; then
  TRIGGER_TYPE="Manual Release"
elif [ "$TRIGGER_TYPE_RAW" == "repository_dispatch" ]; then
  # For repository_dispatch, check if we can determine the specific type
  # If SOURCE contains "Konflux" or "konflux-build-complete", it's a build complete event
  if echo "$SOURCE" | grep -qi "konflux\|build-complete"; then
    TRIGGER_TYPE="Automated Release (Build Complete)"
  else
    TRIGGER_TYPE="Automated Release"
  fi
else
  TRIGGER_TYPE="$TRIGGER_TYPE_RAW"
fi

# Verify GH_TOKEN is set
if [ -z "${GH_TOKEN:-}" ]; then
  echo "Error: GH_TOKEN environment variable is not set"
  exit 1
fi

# Set up GitHub CLI authentication
export GH_TOKEN

# Get repository owner and name from GitHub Actions context
if [ -z "${GITHUB_REPOSITORY:-}" ]; then
  echo "Error: GITHUB_REPOSITORY environment variable is not set"
  exit 1
fi

# Extract repository name from GITHUB_REPOSITORY (format: owner/repo)
REPO_NAME=$(echo "$GITHUB_REPOSITORY" | cut -d'/' -f2)

# Use GITHUB_REPOSITORY_OWNER if available, otherwise extract from GITHUB_REPOSITORY
REPO_OWNER="${GITHUB_REPOSITORY_OWNER:-$(echo "$GITHUB_REPOSITORY" | cut -d'/' -f1)}"

# Build workflow URL
WORKFLOW_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}/actions/runs/${RUN_ID}"

# Capture failure reason if not provided
if [ -z "$FAILURE_REASON" ] && command -v gh >/dev/null 2>&1; then
  # Get failed job/step information from the workflow run
  FAILED_STEP=$(gh run view "${RUN_ID}" \
    --repo "${REPO_OWNER}/${REPO_NAME}" \
    --json jobs \
    --jq '.jobs[] | select(.conclusion == "failure") | .steps[] | select(.conclusion == "failure") | .name' \
    2>/dev/null | head -n1 || echo "")

  if [ -n "$FAILED_STEP" ]; then
    FAILURE_REASON="Failed step: ${FAILED_STEP}"
  fi
fi

# Issue title
ISSUE_TITLE="Release Failure"

# Get current timestamp in ISO format
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build issue body (used for both new issues and comments)
# Use actual newlines instead of \n escape sequences
ISSUE_BODY="Release failed on ${TIMESTAMP}

**Workflow Run:** ${WORKFLOW_URL}
**Run ID:** ${RUN_ID}
**Trigger:** ${TRIGGER_TYPE}

**Release Details:**
- **Version:** ${VERSION}
- **Git Ref:** ${GIT_REF}
- **Image Tag:** ${IMAGE_TAG}
- **Source:** ${SOURCE}"

# Add failure reason section if available
if [ -n "$FAILURE_REASON" ]; then
  ISSUE_BODY="${ISSUE_BODY}

**Failure Reason:**
${FAILURE_REASON}"
fi

# Add closing message
ISSUE_BODY="${ISSUE_BODY}

Please check the workflow logs to identify and fix the issue.

---
*This issue was automatically created by the Create Release workflow*"

# Check for existing open issue with the same title
# List issues with labels 'automated' and 'workflow-failure' (same filtering as update-upstream-manifests.yaml)
ISSUES=$(gh issue list \
  --repo "${REPO_OWNER}/${REPO_NAME}" \
  --state open \
  --label "automated,workflow-failure" \
  --json number,title \
  2>/dev/null || echo "[]")

# Find issue with matching title (same as issues.find(issue => issue.title === title))
EXISTING_ISSUE=$(echo "$ISSUES" | jq -r ".[] | select(.title == \"${ISSUE_TITLE}\") | .number" | head -n1)

if [ -n "$EXISTING_ISSUE" ]; then
  # Append to existing issue body instead of adding a comment
  echo "Found existing issue #${EXISTING_ISSUE}, appending failure message..."

  # Get current issue body
  CURRENT_BODY=$(gh issue view "${EXISTING_ISSUE}" \
    --repo "${REPO_OWNER}/${REPO_NAME}" \
    --json body \
    --jq '.body' 2>/dev/null || echo "")

  # Append new failure message with separator
  UPDATED_BODY="${CURRENT_BODY}

---

${ISSUE_BODY}"

  # Update the issue body
  echo "$UPDATED_BODY" | gh issue edit "${EXISTING_ISSUE}" \
    --repo "${REPO_OWNER}/${REPO_NAME}" \
    --body-file - \
    > /dev/null

  echo "Failure message appended to issue #${EXISTING_ISSUE}"
else
  # Create a new issue
  echo "Creating new issue: ${ISSUE_TITLE}..."

  echo "$ISSUE_BODY" | gh issue create \
    --repo "${REPO_OWNER}/${REPO_NAME}" \
    --title "$ISSUE_TITLE" \
    --label "automated" \
    --label "workflow-failure" \
    --body-file - \
    > /dev/null

  echo "Issue created successfully"
fi
