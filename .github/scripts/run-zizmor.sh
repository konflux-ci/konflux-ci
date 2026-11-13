#!/usr/bin/env bash
# Audit the GitHub Actions workflows a change touches with zizmor.
#
# Usage:
#   run-zizmor.sh [REPO_ROOT]
#
# Only changed workflows are audited, the same way Differential ShellCheck
# handles shell scripts. Findings that already exist on the base branch are not
# the current change's to fix, and auditing the whole repository would block
# unrelated work until every one of them is cleared.
#
# Requires: BASE_SHA and HEAD_SHA in the environment, pipx on PATH, and a full
# git history (actions/checkout with fetch-depth: 0) so the base commit is
# available locally.
set -euo pipefail

# renovate: datasource=pypi depName=zizmor
ZIZMOR_VERSION="1.32.0"

REPO_ROOT="${1:-${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}}"
REPO_ROOT="$(cd "${REPO_ROOT}" && pwd)"
cd "${REPO_ROOT}"

BASE_SHA="${BASE_SHA:-}"
HEAD_SHA="${HEAD_SHA:-}"

if [[ -z "${BASE_SHA}" || -z "${HEAD_SHA}" ]]; then
  echo "ERROR: BASE_SHA and HEAD_SHA must both be set" >&2
  exit 1
fi

# Three-dot diff: compare against the merge base, so commits that landed on the
# base branch after this one branched off are not treated as its changes.
mapfile -t changed < <(
  git diff --name-only --diff-filter=ACMR "${BASE_SHA}...${HEAD_SHA}" -- \
    '.github/workflows/*.yml' '.github/workflows/*.yaml'
)

if [[ ${#changed[@]} -eq 0 ]]; then
  echo "No workflows changed, nothing for zizmor to audit."
  exit 0
fi

echo "Auditing ${#changed[@]} changed workflow(s) with zizmor ${ZIZMOR_VERSION}:"
printf '  %s\n' "${changed[@]}"

pipx install "zizmor==${ZIZMOR_VERSION}"

# --offline keeps the audit reproducible and token free. The online audits query
# the GitHub API, which would make the result depend on repository state rather
# than on the diff under review.
#
# --min-confidence medium is what keeps this gate worth having. The injection
# class this was added for is reported at high confidence, while the low
# confidence audits fire on things like a checkout step that was already in the
# file, which would fail changes that have nothing to do with the finding.
# Lower the floor once the backlog of those is cleared.
#
# --format github turns findings into annotations on the diff, so a reviewer
# sees them on the changed line instead of in the job log.
zizmor --offline --no-progress --min-confidence medium --format github -- "${changed[@]}"

echo "=== zizmor found no issues in the changed workflows ==="
