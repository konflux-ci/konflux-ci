#!/bin/bash
set -euo pipefail

# Create or Update Issue Script
# This script creates or updates a GitHub issue when a workflow fails
#
# Usage:
#   create-or-update-issue.sh <title> <body_template> [additional_info_file]
#
# Arguments:
#   title              - Issue title (required)
#   body_template      - Base body text describing the failure (required)
#   additional_info_file - Optional path to JSON file with additional context
#
# Required Environment Variables:
#   GH_TOKEN                  - GitHub token with issues:write permission
#   GITHUB_REPOSITORY         - Repository in format owner/repo
#   GITHUB_REPOSITORY_OWNER   - Repository owner (optional, extracted from GITHUB_REPOSITORY if not set)
#   GITHUB_RUN_ID             - Workflow run ID
#   GITHUB_EVENT_NAME          - Type of trigger (schedule, workflow_dispatch, repository_dispatch, etc.)
#
# Optional Environment Variables:
#   WORKFLOW_NAME             - Name of the workflow (for footer attribution, defaults to 'workflow')
#   SOURCE                    - Source identifier (for repository_dispatch events)
#   RELEASE_VERSION           - Release version (for release workflows)
#   RELEASE_GIT_REF           - Git ref to release (for release workflows)
#   RELEASE_IMAGE_TAG          - Image tag (for release workflows)
#   RELEASE_SOURCE             - Source of the release (for release workflows)
#
# Example:
#   export GH_TOKEN="your_token"
#   export GITHUB_REPOSITORY="owner/repo"
#   export GITHUB_RUN_ID="123456789"
#   export GITHUB_EVENT_NAME="schedule"
#   create-or-update-issue.sh \
#     "⚠️ Auto Tag Weekly Workflow Failed" \
#     "The scheduled workflow to auto-tag the main branch has failed."
#
# Example with additional info:
#   create-or-update-issue.sh \
#     "⚠️ Upstream Manifests Update Workflow Failed" \
#     "The scheduled workflow to update upstream manifests has failed." \
#     /tmp/component-results.json

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <title> <body_template> [additional_info_file]"
  exit 1
fi

ISSUE_TITLE="$1"
BODY_TEMPLATE="$2"
ADDITIONAL_INFO_FILE="${3:-}"

RUN_ID="${GITHUB_RUN_ID:-unknown}"
TRIGGER_TYPE_RAW="${GITHUB_EVENT_NAME:-unknown}"
WORKFLOW_NAME="${WORKFLOW_NAME:-workflow}"
SOURCE="${SOURCE:-${GITHUB_EVENT_NAME:-unknown}}"
FAILURE_REASON=""

# Optional release information
RELEASE_VERSION="${RELEASE_VERSION:-}"
RELEASE_GIT_REF="${RELEASE_GIT_REF:-}"
RELEASE_IMAGE_TAG="${RELEASE_IMAGE_TAG:-}"
RELEASE_SOURCE="${RELEASE_SOURCE:-}"

# Convert trigger type to meaningful name for better identification
if [ "$TRIGGER_TYPE_RAW" == "workflow_dispatch" ]; then
  TRIGGER_TYPE="Manual"
elif [ "$TRIGGER_TYPE_RAW" == "repository_dispatch" ]; then
  # For repository_dispatch, check if we can determine the specific type
  # If SOURCE contains "Konflux" or "konflux-build-complete", it's a build complete event
  if echo "$SOURCE" | grep -qi "konflux\|build-complete"; then
    TRIGGER_TYPE="Automated (Build Complete)"
  else
    TRIGGER_TYPE="Automated (repository_dispatch)"
  fi
elif [ "$TRIGGER_TYPE_RAW" == "schedule" ]; then
  TRIGGER_TYPE="Scheduled (cron)"
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

# Get current timestamp in ISO format
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Process additional info if provided
ADDITIONAL_INFO=""
if [ -n "$ADDITIONAL_INFO_FILE" ] && [ -f "$ADDITIONAL_INFO_FILE" ]; then
  echo "Reading additional info from: $ADDITIONAL_INFO_FILE"

  # Try to parse as JSON and format component results
  if command -v jq >/dev/null 2>&1; then
    # Check if it's valid JSON
    if jq empty "$ADDITIONAL_INFO_FILE" 2>/dev/null; then
      # Format component results similar to update-upstream-manifests.yaml
      FAILED_COMPONENTS=$(jq -r '.failed[]? | "- \(.component): \(.message)"' "$ADDITIONAL_INFO_FILE" 2>/dev/null || echo "")
      SUCCESSFUL_COMPONENTS=$(jq -r '.successful[]? | "- \(.component): \(.message)"' "$ADDITIONAL_INFO_FILE" 2>/dev/null || echo "")
      UP_TO_DATE_COMPONENTS=$(jq -r '.up_to_date[]? | "- \(.component)"' "$ADDITIONAL_INFO_FILE" 2>/dev/null || echo "")

      if [ -n "$FAILED_COMPONENTS" ]; then
        ADDITIONAL_INFO="${ADDITIONAL_INFO}

**Failed Components:**
${FAILED_COMPONENTS}"
      fi

      if [ -n "$SUCCESSFUL_COMPONENTS" ]; then
        ADDITIONAL_INFO="${ADDITIONAL_INFO}

**Successfully Processed Components:**
${SUCCESSFUL_COMPONENTS}"
      fi

      if [ -n "$UP_TO_DATE_COMPONENTS" ]; then
        ADDITIONAL_INFO="${ADDITIONAL_INFO}

**Up-to-date Components:**
${UP_TO_DATE_COMPONENTS}"
      fi
    else
      # Not valid JSON, include as plain text
      ADDITIONAL_INFO="${ADDITIONAL_INFO}

**Additional Information:**
\`\`\`
$(cat "$ADDITIONAL_INFO_FILE" | head -50)
\`\`\`"
    fi
  else
    # jq not available, include as plain text
    ADDITIONAL_INFO="${ADDITIONAL_INFO}

**Additional Information:**
\`\`\`
$(cat "$ADDITIONAL_INFO_FILE" | head -50)
\`\`\`"
  fi
fi

# Build issue body (used for both new issues and appending to existing ones)
# Use actual newlines instead of \n escape sequences
ISSUE_BODY="Workflow failed on ${TIMESTAMP}

${BODY_TEMPLATE}

**Workflow Run:** ${WORKFLOW_URL}
**Run ID:** ${RUN_ID}
**Trigger:** ${TRIGGER_TYPE}"

# Add release details if available
if [ -n "$RELEASE_VERSION" ] || [ -n "$RELEASE_GIT_REF" ] || [ -n "$RELEASE_IMAGE_TAG" ]; then
  ISSUE_BODY="${ISSUE_BODY}

**Release Details:**
- **Version:** ${RELEASE_VERSION:-unknown}
- **Git Ref:** ${RELEASE_GIT_REF:-unknown}
- **Image Tag:** ${RELEASE_IMAGE_TAG:-unknown}
- **Source:** ${RELEASE_SOURCE:-${SOURCE}}"
fi

# Add additional info (component results, etc.)
ISSUE_BODY="${ISSUE_BODY}${ADDITIONAL_INFO}"

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
*This issue was automatically created by the ${WORKFLOW_NAME}*"

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
