#!/usr/bin/env bash
#
# Download and extract e2e test artifacts from an OpenShift CI (Prow) job.
#
# Usage: download-prow-logs.sh <prow-url-or-gcs-path>
#
# Accepts either:
#   - A full Prow UI URL, e.g.:
#     https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/konflux-ci_konflux-ci/7309/pull-ci-konflux-ci-konflux-ci-main-konflux-e2e-v420-optional/2069007573985529856
#   - A GCS path, e.g.:
#     gs://test-platform-results/pr-logs/pull/konflux-ci_konflux-ci/7309/pull-ci-konflux-ci-konflux-ci-main-konflux-e2e-v420-optional/2069007573985529856
#
# Output directory: /tmp/prow-debug-<build-id>/
#
# Requirements: curl (no gsutil or gcloud auth needed — uses public gcsweb HTTP)
# Optional:     python3 (for parsing junit_operator.xml step results)

set -euo pipefail

GCSWEB_BASE="https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs"

input="${1:?Usage: download-prow-logs.sh <prow-url-or-gcs-path>}"

# Parse the input into a GCS bucket-relative path
if [[ "$input" == gs://* ]]; then
  # gs://test-platform-results/pr-logs/pull/...
  gcs_path="${input#gs://}"
elif [[ "$input" == *"prow.ci.openshift.org/view/gs/"* ]]; then
  # https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/...
  gcs_path="${input#*prow.ci.openshift.org/view/gs/}"
elif [[ "$input" == *"gcsweb-ci.apps.ci"*"/gcs/"* ]]; then
  # https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/...
  gcs_path="${input#*/gcs/}"
else
  echo "ERROR: Cannot parse input URL. Provide a Prow UI URL or gs:// path." >&2
  exit 1
fi

# Remove trailing slash
gcs_path="${gcs_path%/}"

# Extract build ID (last path component) for workdir naming
build_id="${gcs_path##*/}"
workdir="/tmp/prow-debug-${build_id}"
mkdir -p "$workdir"

echo "Prow job artifacts: ${GCSWEB_BASE}/${gcs_path}/" >&2
echo "Output directory:   ${workdir}" >&2
echo "" >&2

# Helper: download a single file from gcsweb.
# gcsweb returns 200 for non-existent paths (showing an HTML directory listing),
# so we verify the response is not HTML before accepting it — unless the file
# itself is supposed to be HTML (e.g. .html extension).
download_file() {
  local remote_path="$1"
  local local_path="$2"
  mkdir -p "$(dirname "$local_path")"
  if curl -sfL "${GCSWEB_BASE}/${remote_path}" -o "$local_path" 2>/dev/null; then
    # Skip HTML check for files with .html extension (they're legitimately HTML)
    if [[ "$local_path" != *.html ]]; then
      if head -1 "$local_path" 2>/dev/null | grep -Eqi '<!doctype|<html'; then
        rm -f "$local_path"
        return 1
      fi
    fi
    return 0
  else
    return 1
  fi
}

# --- Top-level metadata ---
echo "==> Downloading top-level metadata..." >&2
for f in build-log.txt finished.json started.json prowjob.json; do
  if download_file "${gcs_path}/${f}" "${workdir}/${f}"; then
    echo "  ✓ ${f}" >&2
  fi
done

# --- ci-operator.log and junit_operator.xml ---
echo "==> Downloading ci-operator artifacts..." >&2
for f in ci-operator.log junit_operator.xml ci-operator-step-graph.json metadata.json; do
  if download_file "${gcs_path}/artifacts/${f}" "${workdir}/artifacts/${f}"; then
    echo "  ✓ artifacts/${f}" >&2
  fi
done

# --- Detect the test workflow name (e.g. "konflux-e2e-v420-optional") ---
# Parse from the GCS path: .../<job-name>/<build-id>
# The job name often matches the workflow directory under artifacts/
path_without_build="${gcs_path%/*}"
job_full_name="${path_without_build##*/}"
# Try common workflow directory name patterns (shorter/stripped names first,
# since gcsweb always returns 200 we must verify with a real file probe)
workflow_dir=""
candidate_pull="${job_full_name#pull-ci-konflux-ci-konflux-ci-main-}"
candidate_periodic="${job_full_name#periodic-ci-konflux-ci-konflux-ci-main-}"
candidate_periodic_trim1="${candidate_periodic#*-}"
candidate_periodic_trim2="${candidate_periodic_trim1#*-}"
for candidate in \
  "$candidate_pull" \
  "$candidate_periodic_trim2" \
  "$candidate_periodic_trim1" \
  "$candidate_periodic" \
  "$job_full_name"; do
  # Probe for a known step's build-log.txt to verify the directory is real
  probe_url="${GCSWEB_BASE}/${gcs_path}/artifacts/${candidate}/konflux-ci-e2e-tests/build-log.txt"
  probe_content=$(curl -sfL "$probe_url" 2>/dev/null | head -c 512 || true)
  if [[ -n "$probe_content" ]] && ! echo "$probe_content" | grep -Eqi '<!doctype|<html'; then
    workflow_dir="$candidate"
    break
  fi
  # Also try redhat-appstudio-report as fallback probe (in case e2e-tests step didn't run)
  probe_url="${GCSWEB_BASE}/${gcs_path}/artifacts/${candidate}/redhat-appstudio-report/build-log.txt"
  probe_content=$(curl -sfL "$probe_url" 2>/dev/null | head -c 512 || true)
  if [[ -n "$probe_content" ]] && ! echo "$probe_content" | grep -Eqi '<!doctype|<html'; then
    workflow_dir="$candidate"
    break
  fi
done

if [[ -z "$workflow_dir" ]]; then
  echo "WARNING: Could not auto-detect workflow directory under artifacts/." >&2
  echo "  Tried: ${job_full_name} and variants." >&2
  echo "  You may need to browse: ${GCSWEB_BASE}/${gcs_path}/artifacts/" >&2
  echo "" >&2
  echo "${workdir}"
  exit 0
fi

echo "==> Detected workflow directory: ${workflow_dir}" >&2
echo "" >&2

# --- Download step-level build-logs ---
# Key steps for Konflux e2e:
#   konflux-ci-install-operator — operator deployment log
#   konflux-ci-e2e-tests — test execution log
#   redhat-appstudio-gather — cluster state dump (most important)
#   redhat-appstudio-report — JUnit results
#   redhat-appstudio-health-check — pre-test health check

steps=(
  "konflux-ci-install-operator"
  "konflux-ci-e2e-tests"
  "redhat-appstudio-gather"
  "redhat-appstudio-report"
  "redhat-appstudio-health-check"
  "gather-extra"
  "gather-audit-logs"
)

step_base="${gcs_path}/artifacts/${workflow_dir}"

echo "==> Downloading step build-logs..." >&2
for step in "${steps[@]}"; do
  if download_file "${step_base}/${step}/build-log.txt" "${workdir}/steps/${step}/build-log.txt"; then
    echo "  ✓ ${step}/build-log.txt" >&2
  fi
  # Also grab finished.json to check pass/fail
  download_file "${step_base}/${step}/finished.json" "${workdir}/steps/${step}/finished.json" 2>/dev/null || true
done

# --- Download JUnit from redhat-appstudio-report ---
echo "" >&2
echo "==> Downloading JUnit reports..." >&2
for f in junit.xml junit-rp.xml junit-summary.html; do
  if download_file "${step_base}/redhat-appstudio-report/artifacts/${f}" "${workdir}/junit/${f}"; then
    echo "  ✓ junit/${f}" >&2
  fi
done

# --- Download gather artifacts (cluster state) ---
echo "" >&2
echo "==> Downloading redhat-appstudio-gather artifacts (cluster state)..." >&2
echo "    (This may take a moment for large files...)" >&2

gather_base="${step_base}/redhat-appstudio-gather/artifacts"

# Key JSON dumps from the gather step
gather_files=(
  "components.json"
  "applications_appstudio.json"
  "pipelineruns.json"
  "taskruns.json"
  "snapshots.json"
  "integrationtestscenarios.json"
  "releases.json"
  "releaseplans.json"
  "releaseplanadmissions.json"
  "enterprisecontractpolicies.json"
  "repositories.json"
  "pipelines.json"
  "tasks.json"
  "tektonconfigs.json"
  "tektonpipelines.json"
  "tektonchains.json"
  "clusterinterceptors.json"
)

gathered=0
for f in "${gather_files[@]}"; do
  if download_file "${gather_base}/${f}" "${workdir}/gather/${f}"; then
    # Skip empty files (0 bytes)
    if [[ -s "${workdir}/gather/${f}" ]]; then
      gathered=$((gathered + 1))
    else
      rm -f "${workdir}/gather/${f}"
    fi
  fi
done
echo "  Downloaded ${gathered} non-empty gather artifacts" >&2

# --- Summary ---
echo "" >&2
echo "=== Download complete ===" >&2
echo "" >&2
echo "Key files:" >&2
echo "  Build log:        ${workdir}/build-log.txt" >&2
echo "  Operator install: ${workdir}/steps/konflux-ci-install-operator/build-log.txt" >&2
echo "  Test execution:   ${workdir}/steps/konflux-ci-e2e-tests/build-log.txt" >&2
echo "  JUnit XML:        ${workdir}/junit/junit.xml" >&2
echo "  Gather artifacts: ${workdir}/gather/" >&2
echo "" >&2

# Show step pass/fail status from junit_operator.xml if available
junit_op="${workdir}/artifacts/junit_operator.xml"
if [[ -f "$junit_op" ]]; then
  echo "Step results (from junit_operator.xml):" >&2
  python3 - "$junit_op" "$workflow_dir" <<'PYEOF' || echo "  (could not parse junit_operator.xml)" >&2
import xml.etree.ElementTree as ET, sys
tree = ET.parse(sys.argv[1])
wf_prefix = sys.argv[2] + '-'
for tc in tree.iter('testcase'):
    name = tc.get('name', '')
    if not name:
        continue
    failed = tc.find('failure') is not None
    short = name
    if 'Run multi-stage test' in name and ' - ' in name:
        short = name.split(' - ', 1)[1].replace(' container test', '')
        if short.startswith(wf_prefix):
            short = short[len(wf_prefix):]
    mark = '\u2717' if failed else '\u2713'
    suffix = ' (FAILED)' if failed else ''
    print(f'  {mark} {short}{suffix}', file=sys.stderr)
PYEOF
else
  echo "Step results: (junit_operator.xml not available)" >&2
fi

echo "" >&2
echo "${workdir}"
