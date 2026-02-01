#!/bin/bash
set -euo pipefail

# Generates cert-manager and trust-manager manifests under dependencies/ using Helm.
# Also extracts cert-manager CRDs into operator/test/crds/cert-manager/ for envtest.
# Intended for local use and for the Update Third-Party Manifests workflow.
#
# Usage:
#   update-third-party-manifests.sh [REPO_ROOT]
#
# Arguments:
#   REPO_ROOT - Repository root (default: GITHUB_WORKSPACE or git rev-parse --show-toplevel)
#
# Environment:
#   CERT_MANAGER_VERSION   - Required. cert-manager chart version.
#   TRUST_MANAGER_VERSION  - Required. trust-manager chart version.
#
# Requires: helm, yq on PATH.

REPO_ROOT="${1:-${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:?CERT_MANAGER_VERSION is required}"
TRUST_MANAGER_VERSION="${TRUST_MANAGER_VERSION:?TRUST_MANAGER_VERSION is required}"

cd "$REPO_ROOT"

if ! command -v helm &>/dev/null; then
  echo "helm is required but not installed" >&2
  exit 1
fi
if ! command -v yq &>/dev/null; then
  echo "yq is required but not installed" >&2
  exit 1
fi

# Both charts from https://charts.jetstack.io (same source Renovate tracks)
helm repo add jetstack https://charts.jetstack.io --force-update >/dev/null 2>&1

# cert-manager
helm template cert-manager jetstack/cert-manager \
  --version "$CERT_MANAGER_VERSION" \
  --namespace cert-manager \
  --set installCRDs=true \
  --set 'resources.requests.cpu=90m' \
  --set 'resources.requests.memory=90Mi' \
  --set 'resources.limits.cpu=120m' \
  --set 'resources.limits.memory=120Mi' \
  --set 'webhook.resources.requests.cpu=90m' \
  --set 'webhook.resources.requests.memory=90Mi' \
  --set 'webhook.resources.limits.cpu=120m' \
  --set 'webhook.resources.limits.memory=120Mi' \
  --set 'cainjector.resources.requests.cpu=90m' \
  --set 'cainjector.resources.requests.memory=90Mi' \
  --set 'cainjector.resources.limits.cpu=120m' \
  --set 'cainjector.resources.limits.memory=120Mi' \
  --set 'startupapicheck.resources.requests.cpu=10m' \
  --set 'startupapicheck.resources.limits.memory=64Mi' \
  > dependencies/cert-manager/cert-manager.yaml

# Extract cert-manager CRDs for envtest (only CustomResourceDefinition documents)
CRD_DIR="operator/test/crds/cert-manager"
mkdir -p "$CRD_DIR"
yq 'select(.kind == "CustomResourceDefinition")' dependencies/cert-manager/cert-manager.yaml > "$CRD_DIR/cert-manager.crds.yaml"

# trust-manager
helm template trust-manager jetstack/trust-manager \
  --version "$TRUST_MANAGER_VERSION" \
  --namespace cert-manager \
  --set 'resources.requests.cpu=10m' \
  --set 'resources.requests.memory=50Mi' \
  --set 'resources.limits.cpu=100m' \
  --set 'resources.limits.memory=250Mi' \
  --set 'defaultPackage.resources.requests.cpu=10m' \
  --set 'defaultPackage.resources.requests.memory=50Mi' \
  --set 'defaultPackage.resources.limits.cpu=100m' \
  --set 'defaultPackage.resources.limits.memory=250Mi' \
  > dependencies/trust-manager/trust-manager.yaml

echo "Generated dependencies/cert-manager/cert-manager.yaml (cert-manager $CERT_MANAGER_VERSION)"
echo "Generated $CRD_DIR/cert-manager.crds.yaml (extracted CRDs for envtest)"
echo "Generated dependencies/trust-manager/trust-manager.yaml (trust-manager $TRUST_MANAGER_VERSION)"
