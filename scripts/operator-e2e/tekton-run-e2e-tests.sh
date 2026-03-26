#!/usr/bin/env bash
# Tekton tests step: wait for Konflux CR, run test resources + integration + conformance.
# Run from konflux-ci repo root with KUBECONFIG and env set by the Task launcher.
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
cd "${REPO_ROOT}"

: "${E2E_KONFLUX_READY_TIMEOUT:?}"

# kubectl / yq / jq copied by tekton-copy-shared-tools.sh (go-toolset step).
export PATH="/mnt/e2e-shared/bin:${PATH}"

export KUBECONFIG="${KUBECONFIG:-/mnt/e2e-shared/kubeconfig}"
export RELEASE_TA_OCI_STORAGE="${E2E_RELEASE_TA_OCI_STORAGE:-}"
kubectl config current-context

echo "Waiting for Konflux CR to exist..."
for _ in $(seq 1 120); do
  if kubectl get konflux konflux >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! kubectl get konflux konflux >/dev/null 2>&1; then
  echo "Timed out waiting for konflux/konflux resource to be created." >&2
  exit 1
fi

echo "Waiting for Konflux to be Ready (timeout ${E2E_KONFLUX_READY_TIMEOUT})..."
kubectl wait --for=condition=Ready konflux konflux --timeout="${E2E_KONFLUX_READY_TIMEOUT}"

export SKIP_SAMPLE_COMPONENTS=true
./deploy-test-resources.sh

PF_LOG=/mnt/e2e-shared/port-forward.log
PF_PID=""
cleanup() {
  kill "${PF_PID:-}" 2>/dev/null || true
}
trap cleanup EXIT

kubectl port-forward -n konflux-ui svc/proxy 9443:9443 >"${PF_LOG}" 2>&1 &
PF_PID=$!

for _ in $(seq 1 90); do
  if curl -sk --connect-timeout 2 -o /dev/null "https://127.0.0.1:9443/health" 2>/dev/null; then
    echo "Konflux UI port-forward ready (127.0.0.1:9443 -> konflux-ui/proxy:9443)."
    break
  fi
  if ! kill -0 "${PF_PID}" 2>/dev/null; then
    echo "kubectl port-forward exited before becoming ready; log:" >&2
    cat "${PF_LOG}" >&2 || true
    exit 1
  fi
  sleep 1
done
if ! kill -0 "${PF_PID}" 2>/dev/null; then
  echo "kubectl port-forward is not running." >&2
  cat "${PF_LOG}" >&2 || true
  exit 1
fi
if ! curl -sk --connect-timeout 2 -o /dev/null "https://127.0.0.1:9443/health" 2>/dev/null; then
  echo "Timed out waiting for https://localhost:9443/health via port-forward." >&2
  cat "${PF_LOG}" >&2 || true
  exit 1
fi

(cd "${REPO_ROOT}/test/go-tests" && go test . ./pkg/...)
eval "$(bash scripts/operator-e2e/prepare-conformance-env.sh "${REPO_ROOT}")"
export GITHUB_TOKEN="${GH_TOKEN:-}"
export MY_GITHUB_ORG="${GH_ORG:-}"
export QUAY_TOKEN=""
export E2E_APPLICATIONS_NAMESPACE=user-ns2
JUNIT="${REPO_ROOT}/junit-conformance.xml"
bash scripts/operator-e2e/run-conformance-tests.sh "${REPO_ROOT}" "${JUNIT}"
