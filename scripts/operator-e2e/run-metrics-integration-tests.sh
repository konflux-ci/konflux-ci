#!/usr/bin/env bash
# Run cluster metrics integration tests (Prometheus-style HTTPS scrape).
#
# Usage: run-metrics-integration-tests.sh [REPO_ROOT]
#
# Optional env:
#   METRICS_GINKGO_LABEL_FILTER — Ginkgo label filter (default: metrics).
#     Use "metrics && component" on Tekton (operator pod is not running during e2e).
set -euo pipefail

REPO_ROOT="$(cd "${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}" && pwd)"
LABEL_FILTER="${METRICS_GINKGO_LABEL_FILTER:-metrics}"

cd "${REPO_ROOT}/test/go-tests"
export KONFLUX_REPO_ROOT="${REPO_ROOT}"
echo "Running metrics integration tests (label filter: ${LABEL_FILTER})..."
go test -mod=mod ./metricsintegration -v -timeout 15m -ginkgo.label-filter="${LABEL_FILTER}" "$@"
