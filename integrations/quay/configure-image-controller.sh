#!/bin/bash
set -euo pipefail

# Configures image-controller to use the self-hosted Quay instance deployed
# by deploy-deps.sh. Creates a Quay organization, sets up the
# image-controller secret with the admin token and internal API URL, and
# patches the Konflux CR with the Quay CA bundle.
#
# Prerequisites: curl, jq, kubectl
# Requires: Self-hosted Quay deployed (SKIP_QUAY=false in deploy-deps.sh)
#
# Usage:
#   ./integrations/quay/configure-image-controller.sh
#
# Environment variables:
#   QUAY_ORGANIZATION  - Organization name to create (default: konflux)
#   QUAY_URL           - External Quay URL (default: https://localhost:8443)

: "${QUAY_URL:=https://localhost:8443}"
: "${QUAY_ORGANIZATION:=konflux}"

QUAY_INTERNAL_API_URL="https://quay-service.quay/api/v1"

for cmd in curl jq kubectl; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "❌ '$cmd' is required but not found" >&2
        exit 1
    fi
done

if ! kubectl get secret quay-admin-token -n quay &>/dev/null; then
    echo "❌ quay-admin-token secret not found in namespace 'quay'" >&2
    echo "   Deploy Quay first: SKIP_QUAY=false ./scripts/deploy-local.sh" >&2
    exit 1
fi

ADMIN_TOKEN="$(kubectl get secret quay-admin-token -n quay \
    -o jsonpath='{.data.token}' | base64 -d)"

quay_api() {
    local method="$1" path="$2"
    shift 2
    curl -4 -sk --connect-timeout 10 \
        -X "$method" \
        -H "Authorization: Bearer ${ADMIN_TOKEN}" \
        -H "Content-Type: application/json" \
        "$@" \
        "${QUAY_URL}/api/v1${path}"
}

# ── Create organization ──────────────────────────────────────────────────────

echo "📦 Creating organization '${QUAY_ORGANIZATION}'..." >&2
ORG_HTTP_CODE="$(curl -4 -sk --connect-timeout 10 -o /dev/null -w '%{http_code}' \
    -X POST \
    -H "Authorization: Bearer ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"${QUAY_ORGANIZATION}\"}" \
    "${QUAY_URL}/api/v1/organization/")" || true

case "$ORG_HTTP_CODE" in
    200|201)
        echo "✅ Organization '${QUAY_ORGANIZATION}' created" >&2
        ;;
    400)
        echo "✅ Organization '${QUAY_ORGANIZATION}' already exists" >&2
        ;;
    *)
        echo "❌ Failed to create organization (HTTP ${ORG_HTTP_CODE})" >&2
        exit 1
        ;;
esac

# ── Extract CA certificate ────────────────────────────────────────────────────

echo "🔐 Extracting Quay CA certificate..." >&2
CA_CERT="$(kubectl get secret quay-tls -n quay -o jsonpath='{.data.ca\.crt}' | base64 -d)"

if [ -z "$CA_CERT" ]; then
    echo "❌ Could not extract CA certificate from quay-tls secret" >&2
    exit 1
fi
echo "✅ CA certificate extracted" >&2

# ── Create image-controller resources ─────────────────────────────────────────

echo "📝 Configuring image-controller..." >&2

kubectl create namespace image-controller --dry-run=client -o yaml | kubectl apply -f -

kubectl create configmap quay-ca-bundle \
    --namespace=image-controller \
    --from-literal=ca-bundle.crt="$CA_CERT" \
    --dry-run=client -o yaml | kubectl apply -f -
echo "✅ CA bundle ConfigMap created in image-controller namespace" >&2

kubectl create secret generic quaytoken \
    --namespace=image-controller \
    --from-literal=quaytoken="${ADMIN_TOKEN}" \
    --from-literal=organization="${QUAY_ORGANIZATION}" \
    --from-literal=quayapiurl="${QUAY_INTERNAL_API_URL}" \
    --dry-run=client -o yaml | kubectl apply -f -
echo "✅ quaytoken secret created in image-controller namespace" >&2

# ── Patch Konflux CR ──────────────────────────────────────────────────────────

if kubectl get crd konfluxes.konflux.konflux-ci.dev &>/dev/null &&
   kubectl get konflux konflux &>/dev/null; then
    echo "🔧 Patching Konflux CR with Quay CA bundle..." >&2
    kubectl patch konflux konflux --type=merge -p "
spec:
  imageController:
    enabled: true
    spec:
      quayCABundle:
        configMapName: quay-ca-bundle
        key: ca-bundle.crt
"
    echo "✅ Konflux CR patched" >&2

    echo "⏳ Waiting for image-controller pods to be ready..." >&2
    if kubectl wait --for=condition=Ready --timeout=240s \
        -l control-plane=controller-manager -n image-controller pod 2>/dev/null; then
        echo "✅ image-controller is ready" >&2
    else
        echo "⚠️  image-controller pods did not become ready within 4 minutes" >&2
    fi
else
    echo "ℹ️  Konflux CR not found, skipping CR patch" >&2
    echo "   Apply the CR manually with imageController.enabled: true and quayCABundle configured" >&2
fi

echo "" >&2
echo "🎉 Self-hosted Quay integration complete!" >&2
echo "   Organization: ${QUAY_ORGANIZATION}" >&2
echo "   Quay API URL: ${QUAY_INTERNAL_API_URL}" >&2
echo "   Quay UI:      ${QUAY_URL}" >&2
