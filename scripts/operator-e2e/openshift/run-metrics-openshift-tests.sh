#!/usr/bin/env bash
# Run OpenShift metrics integration tests (UWM readiness, ServiceMonitor contract + Prometheus targets).
#
# Usage: run-metrics-openshift-tests.sh [REPO_ROOT]
#
# UWM readiness waits run in the metricsopenshift BeforeSuite (see pkg/metricsopenshift/wait.go).
# Env: UWM_PROM_WAIT_TIMEOUT, UWM_CANARY_WAIT_TIMEOUT, UWM_POLL_INTERVAL, UWM_CANARY_QUERY, UWM_SKIP_CANARY.
# Debug (CI log): UWM_DEBUG_LOG_INTERVAL (seconds, default 60), UWM_DEBUG_DIRECT_SCRAPE=true for direct scrape in failure snapshot, UWM_DEBUG_OPERATOR_LOG_LINES (default 500).
# Resync evidence: every run logs [UWM resync] lines with operand ServiceMonitor resync_at=... before specs (see pkg/metricsopenshift/resync_evidence.go).
#
# Intended for OpenShift CI only (via test/e2e/run-e2e.sh or infra overlay e2e). Not invoked from
# Kind/Tekton operator E2E.
set -euo pipefail

REPO_ROOT="$(cd "${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}" && pwd)"
LABEL_FILTER="${METRICS_OPENSHIFT_GINKGO_LABEL_FILTER:-openshift}"

cd "${REPO_ROOT}/test/go-tests"
echo "Running OpenShift metrics tests (label filter: ${LABEL_FILTER})..."
go test -mod=mod ./metricsopenshift -v -timeout 120m -ginkgo.label-filter="${LABEL_FILTER}"
