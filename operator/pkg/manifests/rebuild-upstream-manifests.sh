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

# Extract CRDs from rendered manifests into test/crds/ for envtest.
# Controllers that Owns() CRs of CRDs they install need the CRD present
# before the manager starts. Follows the cert-manager extraction pattern
# in .github/scripts/update-third-party-manifests.sh.
EC_CRD_DIR="${WORKSPACE_ROOT}/operator/test/crds/enterprise-contract"
mkdir -p "${EC_CRD_DIR}"
yq 'select(.kind == "CustomResourceDefinition")' \
  "${WORKSPACE_ROOT}/operator/pkg/manifests/enterprise-contract/manifests.yaml" \
  > "${EC_CRD_DIR}/enterprisecontractpolicies.appstudio.redhat.com.yaml"
echo "Extracted enterprise-contract CRDs for envtest"

# Release has multiple CRDs; only extract the one needed by envtest (Owns() watch target).
RELEASE_CRD_DIR="${WORKSPACE_ROOT}/operator/test/crds/release"
mkdir -p "${RELEASE_CRD_DIR}"
yq 'select(.kind == "CustomResourceDefinition" and .metadata.name == "releaseserviceconfigs.appstudio.redhat.com")' \
  "${WORKSPACE_ROOT}/operator/pkg/manifests/release/manifests.yaml" \
  > "${RELEASE_CRD_DIR}/releaseserviceconfigs.appstudio.redhat.com.yaml"
echo "Extracted release CRDs for envtest"
