#!/bin/bash
set -euo pipefail

# Check Prow Job Status Script
# Monitors periodic Prow jobs via GCS and creates GitHub issues on failure.
#
# Usage:
#   check-prow-job-status.sh
#
# Environment:
#   GH_TOKEN           - GitHub token for issue creation (required)
#   GITHUB_REPOSITORY  - Repository in format owner/repo (required)

GCS_BUCKET="https://storage.googleapis.com/test-platform-results/logs"
PROW_URL="https://prow.ci.openshift.org/view/gs/test-platform-results/logs"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Jobs to monitor
JOBS=(
  "periodic-ci-konflux-ci-konflux-ci-main-ocp420-konflux-e2e-v420"
  "periodic-ci-konflux-ci-konflux-ci-main-ocp420-arm64-konflux-e2e-v420-arm64"
)

# Verify required environment variables
if [ -z "${GH_TOKEN:-}" ]; then
  echo "Error: GH_TOKEN environment variable is not set"
  exit 1
fi

if [ -z "${GITHUB_REPOSITORY:-}" ]; then
  echo "Error: GITHUB_REPOSITORY environment variable is not set"
  exit 1
fi

echo "Checking status of ${#JOBS[@]} Prow job(s)..."
echo ""

FAILED_JOBS=0

for job_name in "${JOBS[@]}"; do
  echo "========================================"
  echo "Checking: ${job_name}"
  echo "========================================"

  # Get latest build number from GCS
  BUILD_NUM=$(curl -sf "${GCS_BUCKET}/${job_name}/latest-build.txt" 2>/dev/null || echo "")

  if [ -z "$BUILD_NUM" ]; then
    echo "  Warning: Could not fetch latest build number"
    continue
  fi

  echo "  Latest build: ${BUILD_NUM}"

  # Get finished.json for that build
  FINISHED_JSON=$(curl -sf "${GCS_BUCKET}/${job_name}/${BUILD_NUM}/finished.json" 2>/dev/null || echo "")

  if [ -z "$FINISHED_JSON" ]; then
    echo "  Warning: Could not fetch finished.json (job may still be running)"
    continue
  fi

  # Extract result
  RESULT=$(echo "$FINISHED_JSON" | jq -r '.result // empty')
  PASSED=$(echo "$FINISHED_JSON" | jq -r '.passed // empty')
  TIMESTAMP=$(echo "$FINISHED_JSON" | jq -r '.timestamp // empty')

  # Convert timestamp to human-readable format
  if [ -n "$TIMESTAMP" ]; then
    COMPLETED_TIME=$(date -d "@${TIMESTAMP}" -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "$TIMESTAMP")
  else
    COMPLETED_TIME="unknown"
  fi

  JOB_URL="${PROW_URL}/${job_name}/${BUILD_NUM}"

  echo "  Result: ${RESULT}"
  echo "  Passed: ${PASSED}"
  echo "  Completed: ${COMPLETED_TIME}"
  echo "  URL: ${JOB_URL}"

  # Check if job failed
  if [[ "$PASSED" == "false" || "$RESULT" == "FAILURE" || "$RESULT" == "ERROR" ]]; then
    echo "  Job FAILED - creating/updating issue..."
    FAILED_JOBS=$((FAILED_JOBS + 1))

    ISSUE_TITLE="Periodic E2E Test Failed: ${job_name}"
    ISSUE_BODY="**Job:** ${job_name}
**Result:** ${RESULT}
**Completed:** ${COMPLETED_TIME}
**Logs:** ${JOB_URL}"

    export WORKFLOW_NAME="${WORKFLOW_NAME:-Monitor Periodic Prow Jobs}"

    "${SCRIPT_DIR}/create-or-update-issue.sh" \
      "${ISSUE_TITLE}" \
      "${ISSUE_BODY}"

  else
    echo "  Job passed"
  fi

  echo ""
done

echo "========================================"
echo "Summary: ${FAILED_JOBS}/${#JOBS[@]} job(s) failed"
echo "========================================"

if [ $FAILED_JOBS -gt 0 ]; then
  echo "Issues have been created/updated for failed jobs."
fi

exit 0
