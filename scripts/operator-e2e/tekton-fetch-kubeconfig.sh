#!/usr/bin/env bash
# Used by Tekton Task step fetch-kubeconfig: decode cluster kubeconfig from a Secret into a shared volume.
# Env: POD_NAMESPACE (required). Args: secret-name [key-in-secret]
set -euo pipefail

SECRET="${1:?usage: $0 SECRET_NAME [KEY]}"
KEY="${2:-kubeconfig}"
OUT_DIR="${KUBECONFIG_OUT_DIR:-/mnt/e2e-shared}"
OUT_FILE="${OUT_DIR}/kubeconfig"

: "${POD_NAMESPACE:?POD_NAMESPACE must be set (e.g. from downward API)}"

mkdir -p "${OUT_DIR}"

for k in "${KEY}" kubeconfig config value KUBECONFIG; do
  if kubectl get secret "${SECRET}" -n "${POD_NAMESPACE}" -o "jsonpath={.data.${k}}" 2>/dev/null | base64 -d >"${OUT_FILE}" 2>/dev/null; then
    if [[ -s "${OUT_FILE}" ]]; then
      echo "Cluster kubeconfig written to shared volume (do not log file contents)."
      chmod 644 "${OUT_FILE}"
      exit 0
    fi
  fi
done

echo "Could not decode non-empty kubeconfig from secret ${SECRET} in namespace ${POD_NAMESPACE}." >&2
if kubectl get secret "${SECRET}" -n "${POD_NAMESPACE}" -o name >/dev/null 2>&1; then
  echo "Secret exists; kubectl describe (metadata and key sizes only, not values):" >&2
  kubectl describe secret "${SECRET}" -n "${POD_NAMESPACE}" >&2 || true
else
  echo "Secret not found or not readable with current RBAC." >&2
fi
exit 1
