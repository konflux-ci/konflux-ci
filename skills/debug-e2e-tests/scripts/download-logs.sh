#!/usr/bin/env bash
#
# Download and extract e2e test artifacts from a GitHub Actions run.
#
# Usage: download-logs.sh <run-id> [arch]
#   run-id  — GitHub Actions run ID
#   arch    — amd64 (default) or arm64
#
# Output directory: /tmp/e2e-debug-<run-id>/

set -euo pipefail

REPO="konflux-ci/konflux-ci"

run_id="${1:?Usage: download-logs.sh <run-id> [arch]}"
arch="${2:-amd64}"
workdir="/tmp/e2e-debug-${run_id}"

mkdir -p "$workdir"
echo "Downloading artifacts for run ${run_id} (${arch}) into ${workdir}..." >&2

gh run download "$run_id" \
  --repo "$REPO" \
  --name "logs-${arch}" \
  --dir "${workdir}/logs" 2>/dev/null || {
    echo "Warning: could not download logs-${arch} artifact. It may have expired (90-day retention)." >&2
  }

echo "Downloading raw job log for the failing job..." >&2
failing_job_id=$(
  gh api "repos/${REPO}/actions/runs/${run_id}/jobs" \
    --jq ".jobs[] | select(.conclusion==\"failure\" and (.name | test(\"${arch}\"; \"i\"))) | .id" \
  | head -1
) || {
  echo "Warning: could not query jobs for run ${run_id}." >&2
  failing_job_id=""
}

if [[ -n "$failing_job_id" ]]; then
  gh api "repos/${REPO}/actions/jobs/${failing_job_id}/logs" \
    > "${workdir}/job.log" 2>/dev/null || echo "Warning: could not download job log." >&2
  echo "Job log saved to ${workdir}/job.log" >&2
else
  echo "No failing job found matching arch=${arch}. Trying any failed job..." >&2
  failing_job_id=$(
    gh api "repos/${REPO}/actions/runs/${run_id}/jobs" \
      --jq '.jobs[] | select(.conclusion=="failure") | .id' \
    | head -1
  ) || {
    echo "Warning: could not query jobs for run ${run_id}." >&2
    failing_job_id=""
  }
  if [[ -n "$failing_job_id" ]]; then
    gh api "repos/${REPO}/actions/jobs/${failing_job_id}/logs" \
      > "${workdir}/job.log" 2>/dev/null || echo "Warning: could not download job log." >&2
    echo "Job log saved to ${workdir}/job.log" >&2
  else
    echo "No failing jobs found in this run." >&2
  fi
fi

echo "" >&2
echo "=== Download complete ===" >&2
echo "Artifacts directory: ${workdir}/logs/" >&2
echo "Job log:             ${workdir}/job.log" >&2

if [[ -d "${workdir}/logs" ]]; then
  echo "" >&2
  echo "Contents:" >&2
  find "${workdir}/logs" -type f | head -30 >&2
  total=$(find "${workdir}/logs" -type f | wc -l)
  if (( total > 30 )); then
    echo "... and $((total - 30)) more files" >&2
  fi
fi

echo "" >&2
echo "${workdir}"
