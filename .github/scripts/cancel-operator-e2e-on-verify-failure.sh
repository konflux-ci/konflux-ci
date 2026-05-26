#!/usr/bin/env bash
# Cancel in-progress or queued "Operator E2E Tests" workflow runs for the current
# PR head when rendered manifests are out of sync.
#
# Usage (CI only, after verify-manifests-in-sync.sh fails on pull_request):
#   cancel-operator-e2e-on-verify-failure.sh
#
# Required environment:
#   GH_TOKEN              - Token with actions:write
#   GITHUB_REPOSITORY     - owner/repo
#   PR_NUMBER             - Pull request number
#   HEAD_SHA              - PR head commit SHA to match workflow runs
#   HEAD_REF              - PR head branch name (gh run list --branch)
#
# Optional environment:
#   E2E_WORKFLOW_NAME     - Workflow display name (default: Operator E2E Tests)
#   FORCE_RUN_E2E_LABEL   - PR label that skips cancellation (default: force-run-e2e)
#   RETRY_ATTEMPTS        - Scan attempts when E2E is not queued yet (default: 10)
#   RETRY_INTERVAL_SEC    - Seconds between scan attempts (default: 20)
#   GH_API_RETRIES        - Per-request retries for gh API errors (default: 3)
#
# Always exits 0; API/cancellation failures are logged as warnings so verify failure
# remains the only hard job outcome.
#
set -euo pipefail

E2E_WORKFLOW_NAME="${E2E_WORKFLOW_NAME:-Operator E2E Tests}"
FORCE_RUN_E2E_LABEL="${FORCE_RUN_E2E_LABEL:-force-run-e2e}"
RETRY_ATTEMPTS="${RETRY_ATTEMPTS:-10}"
RETRY_INTERVAL_SEC="${RETRY_INTERVAL_SEC:-20}"
GH_API_RETRIES="${GH_API_RETRIES:-3}"

: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${PR_NUMBER:?PR_NUMBER is required}"
: "${HEAD_SHA:?HEAD_SHA is required}"
: "${HEAD_REF:?HEAD_REF is required}"

SUMMARY_FILE="${GITHUB_STEP_SUMMARY:-/dev/null}"

log() {
  echo "$@"
}

append_summary() {
  echo "$1" >>"${SUMMARY_FILE}"
}

gh_retry() {
  local attempt=1
  while true; do
    if "$@"; then
      return 0
    fi
    if (( attempt >= GH_API_RETRIES )); then
      return 1
    fi
    log "gh command failed (attempt ${attempt}/${GH_API_RETRIES}), retrying in 5s..."
    sleep 5
    attempt=$((attempt + 1))
  done
}

# Returns: 0 = force-run-e2e label present, 1 = label absent, 2 = could not read labels.
label_check_status() {
  local label_present=""
  if label_present="$(gh_retry gh pr view "${PR_NUMBER}" \
    --repo "${GITHUB_REPOSITORY}" \
    --json labels \
    --jq "(.labels // []) | any(.name == \"${FORCE_RUN_E2E_LABEL}\")")"; then
    if [[ "${label_present}" == "true" ]]; then
      return 0
    fi
    return 1
  fi
  return 2
}

list_cancellable_run_ids() {
  if ! gh_retry gh run list \
    --repo "${GITHUB_REPOSITORY}" \
    --workflow "${E2E_WORKFLOW_NAME}" \
    --branch "${HEAD_REF}" \
    --limit 30 \
    --json databaseId,headSha,status \
    --jq "[.[] | select(.headSha == \"${HEAD_SHA}\" and (.status == \"in_progress\" or .status == \"queued\")) | .databaseId] | unique | .[]"; then
    log "::warning::Failed to list Operator E2E workflow runs for ${HEAD_REF}@${HEAD_SHA}"
    return 0
  fi
}

cancel_run() {
  local run_id="$1"
  gh_retry gh run cancel "${run_id}" --repo "${GITHUB_REPOSITORY}"
}

if label_check_status; then
  label_status=0
else
  label_status=$?
fi
if [[ "${label_status}" -eq 0 ]]; then
  log "PR #${PR_NUMBER} has label '${FORCE_RUN_E2E_LABEL}'; skipping Operator E2E cancellation."
  append_summary "## Operator E2E cancellation skipped"
  append_summary ""
  append_summary "Label \`${FORCE_RUN_E2E_LABEL}\` is present on this PR."
  exit 0
fi
if [[ "${label_status}" -eq 2 ]]; then
  log "::warning::Failed to read labels on PR #${PR_NUMBER}; skipping Operator E2E cancellation"
  append_summary "## Operator E2E cancellation skipped"
  append_summary ""
  append_summary "Could not read PR labels (missing \`pull-requests: read\` or API error). E2E runs were **not** cancelled."
  exit 0
fi

append_summary "## Operator E2E cancellation"
append_summary ""
append_summary "Rendered manifests are **out of sync**. In-progress or queued **${E2E_WORKFLOW_NAME}** runs for commit \`${HEAD_SHA}\` on branch \`${HEAD_REF}\` are being cancelled."
append_summary ""
append_summary "**Next steps:** Regenerate and commit manifests (or merge the manifest companion PR for dependency bumps). Re-push to trigger verify and E2E again."
append_summary ""
append_summary "> E2E was cancelled because manifests are out of sync; fix verify or use the manifest companion PR."

declare -a cancelled_ids=()
declare -A seen_ids=()

for attempt in $(seq 1 "${RETRY_ATTEMPTS}"); do
  log "Scan attempt ${attempt}/${RETRY_ATTEMPTS} for cancellable Operator E2E runs..."
  found_any=false

  while IFS= read -r run_id; do
    [[ -n "${run_id}" ]] || continue
    if [[ -n "${seen_ids[${run_id}]+x}" ]]; then
      continue
    fi
    found_any=true
    log "Cancelling workflow run ${run_id}..."
    if cancel_run "${run_id}"; then
      seen_ids["${run_id}"]=1
      cancelled_ids+=("${run_id}")
      log "Cancelled run ${run_id}"
    else
      log "::warning::Failed to cancel workflow run ${run_id} after ${GH_API_RETRIES} attempts"
    fi
  done < <(list_cancellable_run_ids || true)

  if (( attempt < RETRY_ATTEMPTS )); then
    sleep "${RETRY_INTERVAL_SEC}"
  fi

  # Keep scanning until the last attempt so we catch runs queued after verify started.
  if [[ "${found_any}" == false && "${attempt}" -lt "${RETRY_ATTEMPTS}" ]]; then
    log "No matching runs yet; waiting ${RETRY_INTERVAL_SEC}s before retry..."
  fi
done

append_summary ""
if ((${#cancelled_ids[@]} > 0)); then
  append_summary "### Cancelled workflow run IDs"
  append_summary ""
  for run_id in "${cancelled_ids[@]}"; do
    append_summary "- [\`${run_id}\`](https://github.com/${GITHUB_REPOSITORY}/actions/runs/${run_id})"
  done
  log "Cancelled ${#cancelled_ids[@]} Operator E2E workflow run(s): ${cancelled_ids[*]}"
else
  append_summary "### Cancelled workflow run IDs"
  append_summary ""
  append_summary "_No in-progress or queued Operator E2E runs were found for this commit after ${RETRY_ATTEMPTS} scan attempts._"
  log "No Operator E2E runs to cancel for ${HEAD_SHA} on ${HEAD_REF}"
fi

exit 0
