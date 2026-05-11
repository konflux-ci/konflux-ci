#!/usr/bin/env bash
# Rebuild operator/pkg/manifests/<component>/manifests.yaml for every top-level
# component under operator/upstream-kustomizations (same layout as process-component.sh).
#
# Usage:
#   rebuild-upstream-manifests.sh <workspace-root>
#
# Requires: kustomize on PATH.
set -euo pipefail

WORKSPACE_ROOT="${1:-}"
if [[ -z "${WORKSPACE_ROOT}" ]]; then
  echo "Usage: $0 <workspace-root>" >&2
  exit 1
fi
WORKSPACE_ROOT="$(cd "${WORKSPACE_ROOT}" && pwd)"

shopt -s nullglob
mapfile -t component_dirs < <(
  for dir in "${WORKSPACE_ROOT}/operator/upstream-kustomizations"/*; do
    if [[ -d "${dir}" ]]; then
      printf '%s\n' "${dir}"
    fi
  done | sort
)

for dir in "${component_dirs[@]}"; do
  component="$(basename "${dir}")"
  out_dir="${WORKSPACE_ROOT}/operator/pkg/manifests/${component}"
  mkdir -p "${out_dir}"
  echo "kustomize build -> operator/pkg/manifests/${component}/manifests.yaml"
  kustomize build "${WORKSPACE_ROOT}/operator/upstream-kustomizations/${component}" \
    > "${out_dir}/manifests.yaml"
done
