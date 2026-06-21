#!/bin/bash
#
# Deploy demo-user fixtures (opt-in), run proxy integration tests, then E2E conformance tests.
# Set E2E_DEPLOY_TEST_RESOURCES=true to run deploy-test-resources.sh (required for Kind Dex proxy-dex tests).
# Extra arguments are forwarded to the conformance "go test" only, e.g.:
#   ./test/e2e/run-e2e.sh -ginkgo.focus="build" -ginkgo.junit-report=report.xml

set -o nounset
set -o errexit
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

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

# Demo-user fixtures (user-ns1/user-ns2) for Dex proxy-dex tests — opt-in via E2E_DEPLOY_TEST_RESOURCES.
case "${E2E_DEPLOY_TEST_RESOURCES:-}" in
1 | true | yes | TRUE | YES)
    SKIP_SAMPLE_COMPONENTS=true "${REPO_ROOT}/deploy-test-resources.sh"
    ;;
*)
    echo "Skipping deploy-test-resources.sh (set E2E_DEPLOY_TEST_RESOURCES=true to enable)" >&2
    ;;
esac

bash "${REPO_ROOT}/scripts/operator-e2e/run-proxy-integration-tests.sh" "${REPO_ROOT}"

echo "Running E2E conformance tests..."
cd "${REPO_ROOT}/test/go-tests"
# -mod=mod overrides GOFLAGS=-mod=vendor that may be present on some systems; this repo doesn't vendor.
go test -mod=mod ./tests/conformance -v -timeout 45m -ginkgo.vv "$@"
