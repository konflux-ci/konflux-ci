#!/bin/bash
#
# Deploy test resources and run E2E conformance tests.
# Extra arguments are forwarded to "go test", e.g.:
#   ./test/e2e/run-e2e.sh -ginkgo.focus="build" -ginkgo.junit-report=report.xml

set -o nounset
set -o errexit
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# When not in GitHub Actions, export the pinned build bundle (same source as CI). TESTREPO_REVISION
# is not set here; GitHub Actions writes it from test/e2e/testrepo-revision, Tekton uses
# export-testrepo-revision-from-pin.sh. Locally omit it for main or eval that script before running.
if [[ -z "${GITHUB_ENV:-}" ]]; then
  # shellcheck disable=SC1090
  eval "$(bash "${REPO_ROOT}/scripts/operator-e2e/prepare-conformance-env.sh" "${REPO_ROOT}")"
fi

# If kubectl is not available, alias oc as kubectl for this session.
if ! command -v kubectl &>/dev/null; then
    if command -v oc &>/dev/null; then
        echo "kubectl not found; aliasing oc as kubectl for this session"
        function kubectl() { oc "$@"; }
        export -f kubectl
    else
        echo "Error: neither kubectl nor oc found in PATH" >&2
        exit 1
    fi
fi

# Deploy test resources (idempotent — safe to run if already deployed)
SKIP_SAMPLE_COMPONENTS="true" "${REPO_ROOT}/deploy-test-resources.sh"

# Run E2E conformance tests
echo "Running E2E conformance tests..."
cd "${REPO_ROOT}/test/go-tests"
# -mod=mod overrides GOFLAGS=-mod=vendor that may be present on some systems; this repo doesn't vendor.
go test -mod=mod ./tests/conformance -v -timeout 45m -ginkgo.vv "$@"

