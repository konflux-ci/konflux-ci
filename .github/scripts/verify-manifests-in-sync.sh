#!/usr/bin/env bash
# Fail if rendered upstream operator manifests or third-party Helm outputs are
# out of date relative to operator/upstream-kustomizations and pinned chart versions.
#
# Usage:
#   verify-manifests-in-sync.sh [REPO_ROOT]
#
# Chart versions come from export-third-party-chart-env.sh (same pins as the scheduled update workflow).
#
# Requires: kustomize, helm, yq on PATH (same expectation as other workflows that
# invoke kustomize / update-third-party-manifests.sh without installing tools).
set -euo pipefail

REPO_ROOT="${1:-${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}}"
REPO_ROOT="$(cd "${REPO_ROOT}" && pwd)"
cd "${REPO_ROOT}"

eval "$(bash "${REPO_ROOT}/.github/scripts/export-third-party-chart-env.sh" "${REPO_ROOT}")"

fail=false

echo "== Verifying upstream kustomize outputs =="
shopt -s nullglob
for dir in "${REPO_ROOT}/operator/upstream-kustomizations"/*; do
  [[ -d "${dir}" ]] || continue
  component="$(basename "${dir}")"
  committed="${REPO_ROOT}/operator/pkg/manifests/${component}/manifests.yaml"
  if [[ ! -f "${committed}" ]]; then
    echo "❌ Missing committed manifest: ${committed}" >&2
    fail=true
    continue
  fi
  tmp="$(mktemp)"
  if ! kustomize build "${REPO_ROOT}/operator/upstream-kustomizations/${component}" >"${tmp}" 2>/tmp/kustomize.err; then
    echo "❌ kustomize build failed for ${component}" >&2
    cat /tmp/kustomize.err >&2 || true
    rm -f "${tmp}" /tmp/kustomize.err
    fail=true
    continue
  fi
  rm -f /tmp/kustomize.err
  if ! diff -u "${committed}" "${tmp}" >/tmp/diff.out 2>&1; then
    echo "❌ Upstream manifest drift for component: ${component}" >&2
    head -n 80 /tmp/diff.out >&2 || cat /tmp/diff.out >&2
    fail=true
  else
    echo "  OK ${component}"
  fi
  rm -f "${tmp}"
done

echo "== Verifying upstream-derived envtest CRDs =="
ec_crd_path="operator/test/crds/enterprise-contract/enterprisecontractpolicies.appstudio.redhat.com.yaml"
mkdir -p "$(dirname "${ec_crd_path}")"
yq 'select(.kind == "CustomResourceDefinition")' \
  operator/pkg/manifests/enterprise-contract/manifests.yaml > "${ec_crd_path}"
if ! git diff --exit-code -- "${ec_crd_path}" 2>/dev/null; then
  echo "❌ Upstream-derived envtest CRD drift (run rebuild-upstream-manifests.sh and commit)." >&2
  git --no-pager diff -- "${ec_crd_path}" >&2 || true
  fail=true
else
  echo "  OK upstream-derived envtest CRDs"
fi

echo "== Verifying third-party Helm outputs =="
export CERT_MANAGER_VERSION TRUST_MANAGER_VERSION PROMETHEUS_OPERATOR_VERSION
bash "${REPO_ROOT}/.github/scripts/update-third-party-manifests.sh" "${REPO_ROOT}"

third_paths=(
  "dependencies/cert-manager/cert-manager.yaml"
  "dependencies/trust-manager/trust-manager.yaml"
  "operator/test/crds/cert-manager/cert-manager.crds.yaml"
  "operator/test/crds/prometheus/servicemonitors.monitoring.coreos.com.yaml"
  "dependencies/prometheus-operator-crds/servicemonitors.monitoring.coreos.com.yaml"
)
if ! git diff --exit-code -- "${third_paths[@]}" 2>/dev/null; then
  echo "❌ Third-party manifest drift (regenerate with update-third-party-manifests.sh and commit)." >&2
  git --no-pager diff -- "${third_paths[@]}" >&2 || true
  fail=true
else
  echo "  OK third-party manifests"
fi

if [[ "${fail}" == true ]]; then
  echo "" >&2
  echo "FAIL: One or more generated manifests are out of sync." >&2
  exit 1
fi

echo "PASS: upstream kustomize outputs and third-party manifests match the tree."
