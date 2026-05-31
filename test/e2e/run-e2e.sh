#!/bin/bash
#
# Deploy test resources and run E2E conformance tests.
# Extra arguments are forwarded to "go test", e.g.:
#   ./test/e2e/run-e2e.sh -ginkgo.focus="build" -ginkgo.junit-report=report.xml

set -o nounset
set -o errexit
set -o pipefail

# Temporary: OpenShift CI/Prow builder images ship Go 1.25 with GOTOOLCHAIN=local baked in.
# Unconditionally override so Go can download the toolchain required by go.mod (currently 1.26).
# Remove once openshift/release uses rhel-9-release-golang-1.26-openshift-* in konflux-ci job configs.
export GOTOOLCHAIN=auto

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

# Deploy test resources (idempotent — safe to run if already deployed)
SKIP_SAMPLE_COMPONENTS="true" "${REPO_ROOT}/deploy-test-resources.sh"

# Run E2E conformance tests
echo "Running E2E conformance tests..."
cd "${REPO_ROOT}/test/go-tests"
echo "DEBUG GOTOOLCHAIN=$GOTOOLCHAIN ($(go env GOTOOLCHAIN))"
# -mod=mod overrides GOFLAGS=-mod=vendor that may be present on some systems; this repo doesn't vendor.
go test -mod=mod ./tests/conformance -v -timeout 45m -ginkgo.vv "$@"

