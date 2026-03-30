#!/usr/bin/env bash
# Run Ginkgo conformance tests. Requires GH_ORG, GH_TOKEN, QUAY_DOCKERCONFIGJSON, RELEASE_CATALOG_TA_QUAY_TOKEN in env.
# QUAY_TOKEN is cleared for this invocation. Optional: RELEASE_TA_OCI_STORAGE, E2E_APPLICATIONS_NAMESPACE.
# Usage: $0 REPO_ROOT [JUNIT_REPORT_PATH]
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT [JUNIT_REPORT_PATH]}" && pwd)"
JUNIT="${2:-${JUNIT_REPORT_PATH:-${GITHUB_WORKSPACE:-$REPO_ROOT}/junit-conformance.xml}}"

export GITHUB_TOKEN="${GH_TOKEN:?GH_TOKEN required}"
export MY_GITHUB_ORG="${GH_ORG:?GH_ORG required}"
export QUAY_TOKEN=''
export E2E_APPLICATIONS_NAMESPACE="${E2E_APPLICATIONS_NAMESPACE:-user-ns2}"

cd "${REPO_ROOT}/test/go-tests"
go test ./tests/conformance -v -timeout 30m \
  -ginkgo.vv \
  -ginkgo.github-output \
  -ginkgo.junit-report="$JUNIT"
