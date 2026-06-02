#!/usr/bin/env bash
# Reject Operator E2E on PRs superseded by a manifest companion or blocked pending
# upstream images (opt out: force-run-e2e).
#
# Usage (CI only, after checkout in operator-test-e2e.yaml):
#   check-operator-e2e-prerequisites.sh
#
# Fork PR rejection runs inline in the workflow (before checkout) so fork heads cannot
# replace this script. Labels are passed from the workflow event payload (trusted).
#
# Required environment:
#   GITHUB_EVENT_NAME
#   GITHUB_STEP_SUMMARY
#
# Optional environment:
#   PR_LABELS_JSON          - JSON array of PR labels (pull_request events)
#   SUPERSEDED_BY_COMPANION_LABEL - default: superseded-by-companion
#   PENDING_UPSTREAM_IMAGE_LABEL  - default: pending-upstream-image
#   FORCE_RUN_E2E_LABEL     - default: force-run-e2e
#
# Exits 1 on superseded-by-companion or pending-upstream-image PRs unless
# force-run-e2e. Exits 0 otherwise.
#
set -euo pipefail

SUPERSEDED_BY_COMPANION_LABEL="${SUPERSEDED_BY_COMPANION_LABEL:-superseded-by-companion}"
PENDING_UPSTREAM_IMAGE_LABEL="${PENDING_UPSTREAM_IMAGE_LABEL:-pending-upstream-image}"
FORCE_RUN_E2E_LABEL="${FORCE_RUN_E2E_LABEL:-force-run-e2e}"
SUMMARY_FILE="${GITHUB_STEP_SUMMARY:-/dev/null}"

if [[ "${GITHUB_EVENT_NAME}" == "pull_request" ]]; then
  labels_json="${PR_LABELS_JSON:-[]}"
  if echo "${labels_json}" | jq -e --arg label "${FORCE_RUN_E2E_LABEL}" \
    'map(.name) | index($label)' >/dev/null; then
    exit 0
  fi

  if echo "${labels_json}" | jq -e --arg label "${SUPERSEDED_BY_COMPANION_LABEL}" \
    'map(.name) | index($label)' >/dev/null; then
    echo "❌ PR has label ${SUPERSEDED_BY_COMPANION_LABEL}; Operator E2E does not run on this PR." >&2
    {
      echo "### Operator E2E not run (superseded by companion)"
      echo ""
      echo "Merge the manifest companion PR instead of this PR."
      echo "Add label \`${FORCE_RUN_E2E_LABEL}\` to run Operator E2E on this PR anyway."
    } >>"${SUMMARY_FILE}"
    exit 1
  fi

  if echo "${labels_json}" | jq -e --arg label "${PENDING_UPSTREAM_IMAGE_LABEL}" \
    'map(.name) | index($label)' >/dev/null; then
    echo "❌ PR has label ${PENDING_UPSTREAM_IMAGE_LABEL}; Operator E2E does not run on this PR." >&2
    {
      echo "### Operator E2E not run (pending upstream image)"
      echo ""
      echo "Manifest companion did not open a companion PR because upstream container image(s) are not yet published."
      echo "Re-run the manifest companion workflow after the upstream image is available."
      echo "Add label \`${FORCE_RUN_E2E_LABEL}\` to run Operator E2E on this PR anyway."
    } >>"${SUMMARY_FILE}"
    exit 1
  fi
fi
