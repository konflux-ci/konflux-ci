#!/usr/bin/env bash
# Tekton (task-runner): optional overrides, then deploy-local.sh with OPERATOR_INSTALL_METHOD=none.
# Expects kubeconfig at KUBECONFIG (default /mnt/e2e-shared/kubeconfig). Run after tekton-copy-shared-tools.sh
# if a later go-toolset step needs shared bin; this step uses image-builtin tools only.
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
cd "${REPO_ROOT}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/tekton-kubectl-kustomize.sh"

: "${E2E_KONFLUX_CR:?}"
: "${E2E_KIND_CLUSTER:?}"
: "${E2E_KONFLUX_READY_TIMEOUT:?}"

export KUBECONFIG="${KUBECONFIG:-/mnt/e2e-shared/kubeconfig}"
kubectl config current-context

export DEPLOY_LOCAL_SKIP_KIND=1
export KIND_CLUSTER="${E2E_KIND_CLUSTER}"
export KONFLUX_CR="${E2E_KONFLUX_CR}"
export KONFLUX_READY_TIMEOUT="${E2E_KONFLUX_READY_TIMEOUT}"
export CONTAINER_TOOL=podman
export OPERATOR_INSTALL_METHOD=none

if [[ -n "${E2E_OVERRIDES_YAML// }" ]]; then
  export OVERRIDES_YAML="${E2E_OVERRIDES_YAML}"
  bash scripts/operator-e2e/apply-overrides-from-yaml.sh "${REPO_ROOT}"
fi

./scripts/deploy-local.sh
