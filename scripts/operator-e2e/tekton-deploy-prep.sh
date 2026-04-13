#!/usr/bin/env bash
# Tekton (go-toolset): optional overrides via Go implementation, then deploy-local.sh
# with OPERATOR_INSTALL_METHOD=none.
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
cd "${REPO_ROOT}"

: "${E2E_KONFLUX_CR:?}"
: "${E2E_KIND_CLUSTER:?}"
: "${E2E_KONFLUX_READY_TIMEOUT:?}"

export PATH="/mnt/e2e-shared/bin:${PATH}"
export KUBECONFIG="${KUBECONFIG:-/mnt/e2e-shared/kubeconfig}"
kubectl config current-context

export DEPLOY_LOCAL_SKIP_KIND=1
export KIND_CLUSTER="${E2E_KIND_CLUSTER}"
export KONFLUX_CR="${E2E_KONFLUX_CR}"
export KONFLUX_READY_TIMEOUT="${E2E_KONFLUX_READY_TIMEOUT}"
export CONTAINER_TOOL=podman
export OPERATOR_INSTALL_METHOD=none

if [[ -n "${E2E_OVERRIDES_YAML// }" ]]; then
  (
    cd "${REPO_ROOT}/operator"
    go run ./cmd/overrides \
      --upstream-dir "${REPO_ROOT}/operator/upstream-kustomizations" \
      --manifests-dir "${REPO_ROOT}/operator/pkg/manifests" \
      --tmp-dir "${REPO_ROOT}/.tmp" \
      --overrides-yaml "${E2E_OVERRIDES_YAML}"
  )
  echo "Image overrides must be available before Konflux ready timeout (${E2E_KONFLUX_READY_TIMEOUT})."
fi

./scripts/deploy-local.sh
