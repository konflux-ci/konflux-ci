#!/bin/bash
set -euo pipefail

# Trigger OpenShift CI Tests Script
# Triggers Prow jobs via the Gangway REST API, polls for completion,
# and returns success only if all jobs pass. Used to gate release promotions.
#
# The script derives the operator image tag from the git SHA of the provided ref.
# Images are tagged with their full commit SHA by Konflux builds.
#
# Usage:
#   trigger-openshift-ci-tests.sh <git_ref> [job_name]
#
# Arguments:
#   git_ref  - Any git reference (tag, branch, SHA) e.g., v0.1.5-rc.2
#   job_name - (Optional) Specific job to trigger. If omitted, triggers all default jobs.
#
# Environment:
#   OPENSHIFT_CI_TOKEN - Gangway API token (required)
#
# Exit codes:
#   0 - All jobs completed successfully
#   1 - One or more jobs failed, were aborted, or encountered an error
#
# Examples:
#   # Trigger all default jobs
#   trigger-openshift-ci-tests.sh v0.1.5-rc.2
#
#   # Trigger a specific job (for individual retry)
#   trigger-openshift-ci-tests.sh v0.1.5-rc.2 "periodic-ci-konflux-ci-konflux-ci-main-ocp420-konflux-e2e-v420"

GANGWAY_URL="https://gangway-ci.apps.ci.l2s4.p1.openshiftapps.com/v1/executions"
OPERATOR_REPO="quay.io/konflux-ci/konflux-operator"
POLL_INTERVAL=60
TIMEOUT_SECONDS=10800  # 3 hours
MAX_RETRIES=5

# Default list of jobs to trigger when no specific job is provided
DEFAULT_JOBS=(
  "periodic-ci-konflux-ci-konflux-ci-main-ocp420-konflux-e2e-v420"
  "periodic-ci-konflux-ci-konflux-ci-main-ocp420-arm64-konflux-e2e-v420-arm64"
)

# JOBS array will be set based on arguments
declare -a JOBS

# Associative arrays to track job state
declare -A JOB_IDS
declare -A JOB_STATUSES

# Signal handler for cleanup (invoked by trap)
# shellcheck disable=SC2329
cleanup() {
  echo ""
  echo "========================================"
  echo "Script interrupted!"
  for job_name in "${JOBS[@]}"; do
    if [ -n "${JOB_IDS[$job_name]:-}" ]; then
      echo "WARNING: Job may still be running: ${job_name}"
      echo "  ID: ${JOB_IDS[$job_name]}"
    fi
  done
  echo "Check: https://prow.ci.openshift.org/"
  echo "========================================"
  exit 1
}

trap cleanup SIGINT SIGTERM SIGHUP

# Validate arguments
if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "Error: invalid arguments"
  echo "Usage: $0 <git_ref> [job_name]"
  echo "Examples:"
  echo "  $0 v0.1.5-rc.2                    # Trigger all default jobs"
  echo "  $0 v0.1.5-rc.2 \"periodic-ci-...\"  # Trigger specific job"
  exit 1
fi

GIT_REF="$1"

# Set JOBS array based on arguments
if [ $# -eq 2 ]; then
  JOBS=("$2")
  echo "Single job mode: ${JOBS[0]}"
else
  JOBS=("${DEFAULT_JOBS[@]}")
  echo "All jobs mode: ${#JOBS[@]} job(s)"
fi

# Verify required environment variables (disable tracing to avoid leaking token)
{ set +x; } 2>/dev/null
if [ -z "${OPENSHIFT_CI_TOKEN:-}" ]; then
  echo "Error: OPENSHIFT_CI_TOKEN environment variable is not set"
  exit 1
fi

# Derive operator image tag from the git SHA of the provided ref
echo "Resolving git SHA for ${GIT_REF}..."
COMMIT_SHA=$(git rev-parse "${GIT_REF}^{commit}" 2>/dev/null) || {
  echo "Error: Failed to resolve git SHA for ref ${GIT_REF}"
  echo "Ensure the ref exists and the repository has been fetched"
  exit 1
}

OPERATOR_IMAGE="${OPERATOR_REPO}:${COMMIT_SHA}"
echo "Commit SHA: ${COMMIT_SHA}"
echo "Operator image: ${OPERATOR_IMAGE}"
echo "(KONFLUX_REF will be derived from image tag by step scripts)"
echo ""

# Trigger all jobs
echo "Triggering ${#JOBS[@]} job(s)..."
echo "========================================"

for job_name in "${JOBS[@]}"; do
  echo "Triggering: ${job_name}"
  
  REQUEST_PAYLOAD=$(jq -n \
    --arg job_name "$job_name" \
    --arg operator_image "$OPERATOR_IMAGE" \
    '{
      job_name: $job_name,
      job_execution_type: "1",
      pod_spec_options: {
        envs: {
          MULTISTAGE_PARAM_OVERRIDE_OPERATOR_IMAGE: $operator_image
        }
      }
    }')

  RETRY_COUNT=0
  JOB_ID=""
  
  while [ -z "$JOB_ID" ] && [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    HTTP_RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
      -H "Authorization: Bearer ${OPENSHIFT_CI_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$REQUEST_PAYLOAD" \
      "${GANGWAY_URL}")

    HTTP_CODE=$(echo "$HTTP_RESPONSE" | tail -n1)
    RESPONSE=$(echo "$HTTP_RESPONSE" | sed '$d')

    if [[ ! "$HTTP_CODE" =~ ^2[0-9][0-9]$ ]]; then
      RETRY_COUNT=$((RETRY_COUNT + 1))
      echo "  HTTP ${HTTP_CODE} error (attempt ${RETRY_COUNT}/${MAX_RETRIES}). Response: ${RESPONSE}"
      [ $RETRY_COUNT -lt $MAX_RETRIES ] && sleep 10
      continue
    fi

    JOB_ID=$(echo "$RESPONSE" | jq -r '.id // empty' 2>/dev/null) || {
      RETRY_COUNT=$((RETRY_COUNT + 1))
      echo "  JSON parse error (attempt ${RETRY_COUNT}/${MAX_RETRIES}). Response: ${RESPONSE}"
      [ $RETRY_COUNT -lt $MAX_RETRIES ] && sleep 10
      continue
    }

    if [ -z "$JOB_ID" ]; then
      RETRY_COUNT=$((RETRY_COUNT + 1))
      echo "  Missing job ID (attempt ${RETRY_COUNT}/${MAX_RETRIES}). Response: ${RESPONSE}"
      [ $RETRY_COUNT -lt $MAX_RETRIES ] && sleep 10
    fi
  done

  if [ -z "$JOB_ID" ]; then
    echo "  ERROR: Failed to trigger after ${MAX_RETRIES} attempts"
    exit 1
  fi

  JOB_IDS[$job_name]="$JOB_ID"
  JOB_STATUSES[$job_name]="PENDING"
  echo "  Triggered: ${JOB_ID}"
done

echo "========================================"
echo ""
echo "All jobs triggered. Polling for completion (timeout: $((TIMEOUT_SECONDS / 3600))h)..."
echo "Monitor: https://prow.ci.openshift.org/"
echo ""

# Poll for all jobs to complete
START_TIME=$(date +%s)
COMPLETED=0

while [ $COMPLETED -lt ${#JOBS[@]} ]; do
  ELAPSED=$(( $(date +%s) - START_TIME ))

  if [ $ELAPSED -ge $TIMEOUT_SECONDS ]; then
    echo ""
    echo "========================================"
    echo "ERROR: Timeout after ${ELAPSED}s"
    for job_name in "${JOBS[@]}"; do
      echo "  ${job_name}: ${JOB_STATUSES[$job_name]}"
    done
    echo "========================================"
    exit 1
  fi

  COMPLETED=0
  
  for job_name in "${JOBS[@]}"; do
    current_status="${JOB_STATUSES[$job_name]}"
    
    # Skip if already in terminal state
    if [[ "$current_status" =~ ^(SUCCESS|FAILURE|ABORTED|ERROR)$ ]]; then
      COMPLETED=$((COMPLETED + 1))
      continue
    fi

    HTTP_RESPONSE=$(curl -s -w "\n%{http_code}" -H "Authorization: Bearer ${OPENSHIFT_CI_TOKEN}" \
      "${GANGWAY_URL}/${JOB_IDS[$job_name]}")
    HTTP_CODE=$(echo "$HTTP_RESPONSE" | tail -n1)
    RESPONSE=$(echo "$HTTP_RESPONSE" | sed '$d')

    if [[ ! "$HTTP_CODE" =~ ^2[0-9][0-9]$ ]]; then
      echo "[$(date '+%H:%M:%S')] ${job_name}: HTTP ${HTTP_CODE} error polling status"
      continue
    fi

    NEW_STATUS=$(echo "$RESPONSE" | jq -r '.job_status // empty' 2>/dev/null || echo "")

    if [ -n "$NEW_STATUS" ] && [ "$NEW_STATUS" != "$current_status" ]; then
      echo "[$(date '+%H:%M:%S')] ${job_name}: ${NEW_STATUS}"
      JOB_STATUSES[$job_name]="$NEW_STATUS"
      
      if [[ "$NEW_STATUS" =~ ^(SUCCESS|FAILURE|ABORTED|ERROR)$ ]]; then
        COMPLETED=$((COMPLETED + 1))
      fi
    fi
  done

  [ $COMPLETED -lt ${#JOBS[@]} ] && sleep "$POLL_INTERVAL"
done

# Report results
echo ""
echo "========================================"
echo "RESULTS"
echo "========================================"
echo "Git Ref: ${GIT_REF}"
echo "Operator Image: ${OPERATOR_IMAGE}"
echo "Duration: ${ELAPSED}s"
echo ""

FAILED=0
for job_name in "${JOBS[@]}"; do
  status="${JOB_STATUSES[$job_name]}"
  if [ "$status" = "SUCCESS" ]; then
    echo "✓ ${job_name}: ${status}"
  else
    echo "✗ ${job_name}: ${status}"
    FAILED=$((FAILED + 1))
  fi
done

echo ""
if [ $FAILED -eq 0 ]; then
  echo "All ${#JOBS[@]} job(s) passed!"
  echo "========================================"
  exit 0
else
  echo "FAILED: ${FAILED}/${#JOBS[@]} job(s) failed"
  echo "========================================"
  exit 1
fi
