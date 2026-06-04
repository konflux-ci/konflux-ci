#!/usr/bin/env bash
set -euo pipefail

# Check that a data-acceptable-bundles digest has a permanent Unix timestamp tag on Quay.
#
# Usage (from repo root):
#   bash skills/update-upstream-deps/scripts/verify-acceptable-bundles.sh <digest-or-policy-line>
#
# Examples:
#   bash skills/update-upstream-deps/scripts/verify-acceptable-bundles.sh sha256:e240d1043e62...
#   bash skills/update-upstream-deps/scripts/verify-acceptable-bundles.sh 'oci::quay.io/.../data-acceptable-bundles:latest@sha256:e240d1043e62...'

QUAY_API="${QUAY_API:-https://quay.io/api/v1}"
REPO="${REPO:-konflux-ci/tekton-catalog/data-acceptable-bundles}"
MAX_PAGE="${MAX_PAGE:-50}"

raw="${1:-}"
if [[ -z "${raw}" ]]; then
  echo "Usage: $0 <sha256:digest-or-policy-oci-line>" >&2
  exit 1
fi

# Normalize: policy lines look like oci::...:latest@sha256:...
DIGEST="${raw##*@}"
DIGEST="${DIGEST#@}"
if [[ "${DIGEST}" =~ ^[a-fA-F0-9]{64}$ ]]; then
  DIGEST="sha256:${DIGEST}"
fi

if ! curl -fsS -o /dev/null "${QUAY_API}/repository/${REPO}/manifest/${DIGEST}"; then
  echo "BLOCK: manifest ${DIGEST} not found on quay.io/${REPO}"
  exit 1
fi

page=1
while (( page <= MAX_PAGE )); do
  if ! json="$(curl -fsS "${QUAY_API}/repository/${REPO}/tag/?onlyActiveTags=true&limit=100&page=${page}")"; then
    echo "ERROR: failed to query Quay API (tags page ${page})" >&2
    exit 1
  fi
  if ! jq -e '.tags | type == "array"' <<< "${json}" >/dev/null; then
    echo "ERROR: unexpected Quay API response (tags page ${page})" >&2
    exit 1
  fi

  permanent="$(jq -r --arg d "${DIGEST}" '
    [.tags[] | select(.manifest_digest == $d and (.name | test("^[0-9]+$")))]
    | .[0].name // empty
  ' <<< "${json}")"
  if [[ -n "${permanent}" ]]; then
    echo "OK: permanent tag ${permanent} -> ${DIGEST}"
    exit 0
  fi

  if [[ "$(jq -r '.has_additional' <<< "${json}")" != "true" ]]; then
    echo "BLOCK: no Unix timestamp tag on quay.io/${REPO} points at ${DIGEST}"
    echo "Wait for build-definitions to publish a new bundle, or ask upstream to re-tag before merge."
    exit 1
  fi
  page=$((page + 1))
done

echo "BLOCK: no Unix timestamp tag found within ${MAX_PAGE} tag pages on quay.io/${REPO} for ${DIGEST}" >&2
exit 1
