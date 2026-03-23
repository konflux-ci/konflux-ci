#!/bin/bash
set -euo pipefail

# Trigger OpenShift CI Tests Script
# Triggers a Prow job via the Gangway REST API, polls for completion, and returns
# the job status. Used to gate release promotions on passing e2e tests.
#
# The script derives the operator image tag from the git SHA of the RC tag.
# Images are tagged with release-sha-<short-sha> by Konflux builds.
#
# Usage:
#   trigger-openshift-ci-tests.sh <rc_tag>
#
# Arguments:
#   rc_tag - Release candidate tag (e.g., v0.1.5-rc.2)
#
# Environment:
#   OPENSHIFT_CI_TOKEN - Gangway API token (required)
#
# Exit codes:
#   0 - Job completed successfully (SUCCESS)
#   1 - Job failed, was aborted, or encountered an error
#
# Example:
#   export OPENSHIFT_CI_TOKEN="your_gangway_token"
#   trigger-openshift-ci-tests.sh v0.1.5-rc.2

GANGWAY_URL="https://gangway-ci.apps.ci.l2s4.p1.openshiftapps.com/v1/executions"
JOB_NAME="periodic-ci-konflux-ci-konflux-ci-main-ocp420-konflux-e2e-v420"
OPERATOR_REPO="quay.io/konflux-ci/konflux-operator"
POLL_INTERVAL=60
TIMEOUT_SECONDS=10800  # 3 hours
MAX_RETRIES=5

PROWJOB_ID=""

# Signal handler for cleanup
cleanup() {
  echo ""
  echo "========================================"
  echo "Script interrupted!"
  if [ -n "$PROWJOB_ID" ]; then
    echo "WARNING: Prow job may still be running"
    echo "Job ID: ${PROWJOB_ID}"
    echo "Check: https://prow.ci.openshift.org/?job=${JOB_NAME}"
  fi
  echo "========================================"
  exit 1
}

trap cleanup SIGINT SIGTERM SIGHUP

# Validate arguments
if [ $# -ne 1 ]; then
  echo "Error: RC tag argument required"
  echo "Usage: $0 <rc_tag>"
  echo "Example: $0 v0.1.5-rc.2"
  exit 1
fi

RC_TAG="$1"

# Verify required environment variables
if [ -z "${OPENSHIFT_CI_TOKEN:-}" ]; then
  echo "Error: OPENSHIFT_CI_TOKEN environment variable is not set"
  exit 1
fi

# Derive operator image tag from the git SHA of the RC tag
# Images are tagged with: <full-sha>, <short-sha>, and release-sha-<short-sha>
# Use ^{commit} to dereference annotated tags to their underlying commit
echo "Resolving git SHA for ${RC_TAG}..."
COMMIT_SHA=$(git rev-parse "${RC_TAG}^{commit}" 2>/dev/null) || {
  echo "Error: Failed to resolve git SHA for tag ${RC_TAG}"
  echo "Ensure the tag exists and the repository has been fetched with tags"
  exit 1
}

SHORT_SHA="${COMMIT_SHA:0:7}"
OPERATOR_IMAGE="${OPERATOR_REPO}:release-sha-${SHORT_SHA}"
echo "Commit SHA: ${COMMIT_SHA}"
echo "Operator image: ${OPERATOR_IMAGE}"

echo "Triggering Prow job: ${JOB_NAME}"

# Build request payload with operator image override
REQUEST_PAYLOAD=$(jq -n \
  --arg job_name "$JOB_NAME" \
  --arg operator_image "$OPERATOR_IMAGE" \
  '{
    job_name: $job_name,
    job_execution_type: "1",
    pod_spec_options: {
      envs: {
        OPERATOR_IMAGE: $operator_image
      }
    }
  }')

# Trigger the job with retries
RETRY_COUNT=0
while [ -z "$PROWJOB_ID" ] && [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  RESPONSE=$(curl -s -X POST \
    -H "Authorization: Bearer ${OPENSHIFT_CI_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$REQUEST_PAYLOAD" \
    "${GANGWAY_URL}")

  PROWJOB_ID=$(echo "$RESPONSE" | jq -r '.id // empty')

  if [ -z "$PROWJOB_ID" ]; then
    RETRY_COUNT=$((RETRY_COUNT + 1))
    echo "Failed to trigger job (attempt ${RETRY_COUNT}/${MAX_RETRIES}). Response: ${RESPONSE}"
    [ $RETRY_COUNT -lt $MAX_RETRIES ] && sleep 60
  fi
done

if [ -z "$PROWJOB_ID" ]; then
  echo "Error: Failed to trigger Prow job after ${MAX_RETRIES} attempts"
  exit 1
fi

JOB_URL="https://prow.ci.openshift.org/?job=${JOB_NAME}"

echo "Job triggered successfully!"
echo "Job ID: ${PROWJOB_ID}"
echo "RC Tag: ${RC_TAG}"
echo "Operator Image: ${OPERATOR_IMAGE}"
echo "Monitor: ${JOB_URL}"
echo "Polling for completion (timeout: $((TIMEOUT_SECONDS / 3600))h)..."

# Poll for job completion
START_TIME=$(date +%s)
LAST_STATUS=""

while true; do
  ELAPSED=$(( $(date +%s) - START_TIME ))

  if [ $ELAPSED -ge $TIMEOUT_SECONDS ]; then
    echo "Error: Timeout after ${ELAPSED}s. Job may still be running."
    echo "Check: ${JOB_URL}"
    exit 1
  fi

  RESPONSE=$(curl -s -H "Authorization: Bearer ${OPENSHIFT_CI_TOKEN}" "${GANGWAY_URL}/${PROWJOB_ID}")
  JOB_STATUS=$(echo "$RESPONSE" | jq -r '.job_status // empty')

  if [ -z "$JOB_STATUS" ]; then
    sleep "$POLL_INTERVAL"
    continue
  fi

  [ "$JOB_STATUS" != "$LAST_STATUS" ] && echo "[$(date '+%H:%M:%S')] Status: ${JOB_STATUS}"
  LAST_STATUS="$JOB_STATUS"

  case "$JOB_STATUS" in
    SUCCESS)
      echo ""
      echo "========================================"
      echo "SUCCESS: E2E tests passed"
      echo "RC Tag: ${RC_TAG}"
      echo "Operator Image: ${OPERATOR_IMAGE}"
      echo "Duration: ${ELAPSED}s"
      echo "Results: ${JOB_URL}"
      echo "========================================"
      exit 0
      ;;
    FAILURE|ABORTED|ERROR)
      echo ""
      echo "========================================"
      echo "FAILED: E2E tests failed with status ${JOB_STATUS}"
      echo "RC Tag: ${RC_TAG}"
      echo "Operator Image: ${OPERATOR_IMAGE}"
      echo "Duration: ${ELAPSED}s"
      echo "Results: ${JOB_URL}"
      echo "========================================"
      exit 1
      ;;
    *)
      sleep "$POLL_INTERVAL"
      ;;
  esac
done
