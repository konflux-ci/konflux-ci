#!/usr/bin/env bash
# Verify the generated OLM bundle does not require metrics-server-cert without a
# way for OLM to create that Secret.
#
# Background: config/default (Kind/local/rings) mounts Secret metrics-server-cert
# with optional:false via the manager-metrics-certs component. config/manifests
# must omit that component so the OLM bundle does not require a Secret that OLM
# cannot create (DeployableByOLM would leave the manager in ContainerCreating).
#
# Usage (from repository root):
#   .github/scripts/operator-verify-olm-bundle.sh
#
# Requires: make, yq, and the operator Makefile toolchain (operator-sdk, kustomize).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OPERATOR_DIR="${ROOT_DIR}/operator"
BUNDLE_DIR="${OPERATOR_DIR}/bundle"
METRICS_SECRET="metrics-server-cert"

if ! command -v yq >/dev/null 2>&1; then
  echo "::error::yq is required but was not found on PATH." >&2
  exit 1
fi

cleanup() {
  # make bundle may tweak the manager image pin and CSV bases, and writes
  # gitignored bundle outputs. Only restore those side effects — do not reset
  # the whole operator/ tree (would wipe in-progress fixes under config/).
  git -C "${ROOT_DIR}" checkout -- \
    operator/config/manager/kustomization.yaml \
    operator/config/manifests/bases \
    >/dev/null 2>&1 || true
  rm -rf "${BUNDLE_DIR}" "${OPERATOR_DIR}/bundle.Dockerfile"
}
trap cleanup EXIT

echo "=== Generating OLM bundle ==="
# Fixed image/version: only the Deployment/Secret wiring matters for this check.
make -C "${OPERATOR_DIR}" bundle \
  IMG=example.com/konflux-operator:ci \
  VERSION=0.0.0-ci

CSV="$(find "${BUNDLE_DIR}/manifests" -name '*clusterserviceversion.yaml' -print -quit)"
if [[ -z "${CSV}" || ! -f "${CSV}" ]]; then
  echo "::error::No ClusterServiceVersion found under ${BUNDLE_DIR}/manifests" >&2
  exit 1
fi
echo "Using CSV: ${CSV}"

echo "=== Checking metrics-server-cert wiring in OLM bundle ==="

REQUIRED_MOUNTS="$(
  yq -r "
    .spec.install.spec.deployments[]?.spec.template.spec.volumes[]?
    | select(.secret.secretName == \"${METRICS_SECRET}\")
    | select(.secret.optional != true)
    | .name
  " "${CSV}"
)"

CERT_SECRETS="$(
  # Concatenate manifests so multi-doc YAML and many files are scanned together.
  # shellcheck disable=SC2016
  yq -r '
    select(.kind == "Certificate")
    | .spec.secretName // ""
    | select(. != "")
  ' "${BUNDLE_DIR}/manifests"/*.yaml | sort -u
)"

has_metrics_certificate=false
while IFS= read -r secret; do
  [[ -z "${secret}" ]] && continue
  if [[ "${secret}" == "${METRICS_SECRET}" ]]; then
    has_metrics_certificate=true
    break
  fi
done <<<"${CERT_SECRETS}"

if [[ -n "${REQUIRED_MOUNTS}" && "${has_metrics_certificate}" != "true" ]]; then
  echo "❌ FAIL: OLM bundle requires Secret '${METRICS_SECRET}' but does not include a Certificate that creates it." >&2
  echo >&2
  echo "Required volume(s) in ${CSV}:" >&2
  while IFS= read -r name; do
    [[ -z "${name}" ]] && continue
    echo "  - ${name}" >&2
  done <<<"${REQUIRED_MOUNTS}"
  echo >&2
  echo "Certificates in bundle (spec.secretName):" >&2
  if [[ -z "${CERT_SECRETS}" ]]; then
    echo "  (none)" >&2
  else
    while IFS= read -r secret; do
      [[ -z "${secret}" ]] && continue
      echo "  - ${secret}" >&2
    done <<<"${CERT_SECRETS}"
  fi
  echo >&2
  echo "This breaks DeployableByOLM: the manager pod stays ContainerCreating waiting for the Secret." >&2
  echo "config/manifests must compose operator-rbac + user-rbac without the" >&2
  echo "manager-metrics-certs component (that component is only for config/default)." >&2
  exit 1
fi

echo "✅ PASS: OLM bundle does not require '${METRICS_SECRET}' without a creating Certificate."
