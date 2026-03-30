#!/usr/bin/env bash
# kubectl / kind version and namespace list (sanity after deploy).
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"

(
  cd "${REPO_ROOT}/operator"
  kubectl version --client=true 2>/dev/null || kubectl version
  if command -v kind >/dev/null 2>&1; then
    kind version
  else
    echo "kind: not on PATH (skipped in Tekton go-toolset step)"
  fi
)

kubectl get namespace
