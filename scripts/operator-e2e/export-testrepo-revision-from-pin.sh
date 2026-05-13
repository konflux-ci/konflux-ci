#!/usr/bin/env bash
# When TESTREPO_REVISION is unset and test/e2e/testrepo-revision exists, set it from the
# pin file (first non-# line after stripping comment-only lines).
# - If GITHUB_ENV is set (GitHub Actions): append TESTREPO_REVISION=... to that file.
# - Otherwise: print export TESTREPO_REVISION=... for eval (same idea as prepare-conformance-env.sh).
# Usage: eval "$(bash scripts/operator-e2e/export-testrepo-revision-from-pin.sh REPO_ROOT)"
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
PIN="${REPO_ROOT}/test/e2e/testrepo-revision"

if [[ -n "${TESTREPO_REVISION:-}" ]] || [[ ! -f "${PIN}" ]]; then
	exit 0
fi

_tr="$(grep -v '^[[:space:]]*#' "${PIN}" | head -1 | tr -d '[:space:]')"
if [[ -z "${_tr}" ]]; then
	exit 0
fi

if [[ -n "${GITHUB_ENV:-}" ]]; then
	printf 'TESTREPO_REVISION=%s\n' "${_tr}" >>"${GITHUB_ENV}"
else
	printf 'export TESTREPO_REVISION=%q\n' "${_tr}"
fi
