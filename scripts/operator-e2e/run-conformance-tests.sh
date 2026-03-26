#!/usr/bin/env bash
# Run Ginkgo conformance tests. Requires GH_ORG, GH_TOKEN.
# QUAY_TOKEN is cleared for this invocation. Optional: RELEASE_TA_OCI_STORAGE, E2E_APPLICATIONS_NAMESPACE.
# Optional env E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS: extra arguments appended to go test (space-separated),
# e.g. -ginkgo.focus=Name -ginkgo.skip=Other (same idea as ./test/e2e/run-e2e.sh forwarding "$@").
# Usage: $0 REPO_ROOT [JUNIT_REPORT_PATH]
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT [JUNIT_REPORT_PATH]}" && pwd)"
JUNIT="${2:-${JUNIT_REPORT_PATH:-${GITHUB_WORKSPACE:-$REPO_ROOT}/junit-conformance.xml}}"

export GITHUB_TOKEN="${GH_TOKEN:?GH_TOKEN required}"
export MY_GITHUB_ORG="${GH_ORG:?GH_ORG required}"
export QUAY_TOKEN=''
export E2E_APPLICATIONS_NAMESPACE="${E2E_APPLICATIONS_NAMESPACE:-user-ns2}"

cd "${REPO_ROOT}/test/go-tests"
# Deliberate word-splitting: each space-separated flag must be its own argv token for go test.
# Quoting ${E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS} would pass one broken argument (e.g. -ginkgo.focus=...).
# shellcheck disable=SC2086
go test ./tests/conformance -v -timeout 40m \
  -ginkgo.vv \
  -ginkgo.github-output \
  -ginkgo.junit-report="$JUNIT" \
  ${E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS:-}
