#!/usr/bin/env bash
# Tekton (go-toolset): make install/RBAC/build, out-of-cluster bin/manager, apply Konflux CR, wait Ready.
# Expects /mnt/e2e-shared/bin from tekton-copy-shared-tools.sh (kubectl for this image).
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
cd "${REPO_ROOT}"

export PATH="/mnt/e2e-shared/bin:${PATH}"
export KUBECONFIG="${KUBECONFIG:-/mnt/e2e-shared/kubeconfig}"
kubectl config current-context

: "${E2E_KONFLUX_CR:?}"
: "${E2E_KONFLUX_READY_TIMEOUT:?}"

OP_PID=""
cleanup() {
  if [[ -n "${OP_PID:-}" ]]; then
    kill "${OP_PID}" 2>/dev/null || true
    wait "${OP_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

OPERATOR_LOG=/mnt/e2e-shared/operator-manager.log
echo "Out-of-cluster operator: make install, install-user-rbac, build (foreground)..."
(
  cd "${REPO_ROOT}/operator"
  make install
  make install-user-rbac
  make build
)
echo "Starting bin/manager (logs: ${OPERATOR_LOG})..."
(
  cd "${REPO_ROOT}/operator"
  exec ./bin/manager
) >"${OPERATOR_LOG}" 2>&1 &
OP_PID=$!
sleep 5
echo "Applying Konflux CR: ${E2E_KONFLUX_CR}"
kubectl apply -f "${REPO_ROOT}/${E2E_KONFLUX_CR}"
echo "Waiting for Konflux to be Ready (timeout ${E2E_KONFLUX_READY_TIMEOUT})..."
if ! kubectl wait --for=condition=Ready konflux konflux --timeout="${E2E_KONFLUX_READY_TIMEOUT}"; then
  echo "Konflux CR did not become Ready; operator log tail:" >&2
  tail -c 12000 "${OPERATOR_LOG}" >&2 || true
  exit 1
fi
echo "✓ Konflux is ready (out-of-cluster operator)"
