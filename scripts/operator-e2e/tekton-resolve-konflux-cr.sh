#!/usr/bin/env bash
# Resolve Konflux CR path for Tekton deploy-konflux (ConfigMap env or file in clone).
#
# Precedence:
#   1. E2E_KONFLUX_CR_YAML (non-empty after stripping whitespace) from optional ConfigMap
#      (task stepTemplate configMapKeyRef) -> write to E2E_SHARED_DIR/konflux-cr.yaml
#   2. E2E_KONFLUX_CR_PATH relative to REPO_ROOT (or absolute path if already absolute)
#
# Usage:
#   E2E_KONFLUX_CR=$(bash scripts/operator-e2e/tekton-resolve-konflux-cr.sh REPO_ROOT)
#
# Environment:
#   E2E_KONFLUX_CR_YAML   - optional Konflux CR from ConfigMap (set by Tekton when CM exists)
#   E2E_KONFLUX_CR_PATH   - path relative to REPO_ROOT when YAML env is unset/empty
#   E2E_SHARED_DIR        - writable shared volume (default: /mnt/e2e-shared)
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
E2E_KONFLUX_CR_YAML="${E2E_KONFLUX_CR_YAML:-}"
E2E_KONFLUX_CR_PATH="${E2E_KONFLUX_CR_PATH:-operator/config/samples/konflux-e2e.yaml}"
E2E_SHARED_DIR="${E2E_SHARED_DIR:-/mnt/e2e-shared}"

if [[ -n "${E2E_KONFLUX_CR_YAML//[[:space:]]/}" ]]; then
  mkdir -p "${E2E_SHARED_DIR}"
  OUT="${E2E_SHARED_DIR}/konflux-cr.yaml"
  printf '%s' "${E2E_KONFLUX_CR_YAML}" >"${OUT}"
  echo "Using Konflux CR from ConfigMap (${OUT})" >&2
  echo "${OUT}"
  exit 0
fi

CR="${E2E_KONFLUX_CR_PATH}"
if [[ "${CR}" != /* ]]; then
  CR="${REPO_ROOT}/${CR}"
fi

if [[ ! -f "${CR}" ]]; then
  echo "ERROR: Konflux CR file not found: ${CR}" >&2
  exit 1
fi

echo "Using Konflux CR from clone (${CR})" >&2
echo "${CR}"
