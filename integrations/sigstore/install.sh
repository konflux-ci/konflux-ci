#!/bin/bash
set -euo pipefail

# Deploys the Sigstore stack (Fulcio, Rekor, CT Log, Trillian) using the
# scaffold Helm chart. If a Konflux CR is present on the cluster, it is
# patched with the in-cluster Sigstore service URLs.
#
# Prerequisites: helm, kubectl
#
# Usage:
#   ./integrations/sigstore/install.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# renovate: datasource=helm registryUrl=https://sigstore.github.io/helm-charts depName=scaffold
SCAFFOLD_VERSION="0.6.103"

HELM_REPO_NAME="sigstore"
HELM_REPO_URL="https://sigstore.github.io/helm-charts"
HELM_RELEASE_NAME="sigstore-scaffold"

# In-cluster service URLs created by the scaffold chart.
# These match the fullnameOverride / forceNamespace defaults in the scaffold
# chart values.  If you override those in values.yaml, update these accordingly.
FULCIO_URL="http://fulcio-server.fulcio-system.svc.cluster.local"
REKOR_URL="http://rekor-server.rekor-system.svc.cluster.local"
TUF_URL="http://tuf-server.tuf-system.svc.cluster.local"

for tool in helm kubectl; do
    if ! command -v "$tool" &> /dev/null; then
        echo "Error: $tool is required but not installed" >&2
        exit 1
    fi
done

# Detect the cluster's OIDC issuer URL from the API server discovery endpoint.
# This is used to configure both Fulcio's accepted OIDC issuers and the Konflux CR.
OIDC_ISSUER="$(kubectl get --raw /.well-known/openid-configuration | grep -o '"issuer":"[^"]*"' | cut -d'"' -f4)"
if [ -z "${OIDC_ISSUER}" ]; then
    echo "Warning: could not detect OIDC issuer, falling back to https://kubernetes.default.svc" >&2
    OIDC_ISSUER="https://kubernetes.default.svc"
fi
echo "Detected OIDC issuer: ${OIDC_ISSUER}" >&2

echo "🔏 Deploying Sigstore (scaffold v${SCAFFOLD_VERSION})..." >&2
helm repo add "${HELM_REPO_NAME}" "${HELM_REPO_URL}" --force-update

helm upgrade --install "${HELM_RELEASE_NAME}" "${HELM_REPO_NAME}/scaffold" \
    --namespace sigstore-system --create-namespace \
    --version "${SCAFFOLD_VERSION}" \
    --values "${SCRIPT_DIR}/values.yaml" \
    --set-json "fulcio.config.contents.OIDCIssuers={\"${OIDC_ISSUER}\":{\"IssuerURL\":\"${OIDC_ISSUER}\",\"ClientID\":\"sigstore\",\"Type\":\"kubernetes\"}}" \
    --wait \
    --timeout 15m

echo "✅ Sigstore deployed successfully" >&2

# Patch the Konflux CR with Sigstore service URLs (best-effort)
if kubectl get crd konfluxes.konflux.konflux-ci.dev &>/dev/null && \
   kubectl get konflux konflux &>/dev/null; then
    echo "🔧 Patching Konflux CR with Sigstore service URLs..." >&2
    kubectl patch konflux konflux --type=merge -p "
spec:
  info:
    spec:
      clusterConfig:
        data:
          fulcioInternalUrl: ${FULCIO_URL}
          fulcioExternalUrl: ${FULCIO_URL}
          rekorInternalUrl: ${REKOR_URL}
          rekorExternalUrl: ${REKOR_URL}
          tufInternalUrl: ${TUF_URL}
          tufExternalUrl: ${TUF_URL}
          defaultOIDCIssuer: ${OIDC_ISSUER}
          buildIdentityRegexp: "^build-pipeline-[a-z0-9-]+$"
"
    echo "✅ Konflux CR patched with Sigstore URLs" >&2
else
    echo "ℹ️  Konflux CR not found, skipping CR patch" >&2
fi
