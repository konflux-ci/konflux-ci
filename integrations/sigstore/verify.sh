#!/bin/bash
set -euo pipefail

# Verifies an OCI image built by Konflux:
#   1. Image signature  (cosign verify)
#   2. SBOM attestation (cosign verify-attestation --type spdxjson)
#   3. SLSA provenance  (cosign verify-attestation --type slsaprovenance)
#
# Steps 1-2 use the build pipeline identity; step 3 uses the Tekton Chains
# controller identity since Chains signs the provenance attestation itself.
#
# Because the Sigstore services are not exposed via ingress, this script
# sets up kubectl port-forwards to Rekor and TUF before running cosign.
#
# Prerequisites: cosign, kubectl, curl
#
# Usage:
#   ./integrations/sigstore/verify.sh <image-reference>
#
# Example:
#   INSECURE_REGISTRY=true ./integrations/sigstore/verify.sh \
#       localhost:5001/test-component:on-pr-abc123
#
# Environment variables (optional):
#   CERTIFICATE_IDENTITY_REGEXP  - signing identity regexp (default: .*)
#   CERTIFICATE_OIDC_ISSUER      - OIDC issuer (default: auto-detected)
#   CHAINS_IDENTITY - Tekton Chains SA identity for provenance verification
#   REKOR_PORT  - local port for Rekor  (default: 30800)
#   TUF_PORT    - local port for TUF    (default: 30801)
#   INSECURE_REGISTRY - "true" for insecure registries (default: false)

IMAGE_REF="${1:-}"
if [ -z "${IMAGE_REF}" ]; then
    echo "Usage: $0 <image-reference>" >&2
    exit 1
fi

for tool in cosign kubectl curl; do
    if ! command -v "$tool" &>/dev/null; then
        echo "Error: $tool is required but not installed" >&2
        exit 1
    fi
done

# ---------- configuration ----------

REKOR_PORT="${REKOR_PORT:-30800}"
TUF_PORT="${TUF_PORT:-30801}"
REKOR_LOCAL_URL="http://localhost:${REKOR_PORT}"
TUF_LOCAL_URL="http://localhost:${TUF_PORT}"
INSECURE_REGISTRY="${INSECURE_REGISTRY:-false}"

if [ -z "${CERTIFICATE_OIDC_ISSUER:-}" ]; then
    CERTIFICATE_OIDC_ISSUER="$(kubectl get --raw /.well-known/openid-configuration 2>/dev/null \
        | grep -o '"issuer":"[^"]*"' | cut -d'"' -f4 || true)"
    if [ -z "${CERTIFICATE_OIDC_ISSUER}" ]; then
        CERTIFICATE_OIDC_ISSUER="https://kubernetes.default.svc"
    fi
    echo "Auto-detected OIDC issuer: ${CERTIFICATE_OIDC_ISSUER}" >&2
fi
CERTIFICATE_IDENTITY_REGEXP="${CERTIFICATE_IDENTITY_REGEXP:-.*}"
CHAINS_IDENTITY="${CHAINS_IDENTITY:-https://kubernetes.io/namespaces/tekton-pipelines/serviceaccounts/tekton-chains-controller}"

# ---------- port-forwards ----------

PORT_FORWARD_PIDS=()
cleanup() {
    for pid in "${PORT_FORWARD_PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    done
}
trap cleanup EXIT

wait_for_port() {
    local port="$1" name="$2"
    echo "Waiting for ${name} on localhost:${port}..." >&2
    for ((i = 1; i <= 30; i++)); do
        if curl -s -f -o /dev/null "http://localhost:${port}/" 2>/dev/null; then
            echo "${name} is ready" >&2
            return 0
        fi
        sleep 1
    done
    echo "Error: ${name} did not become ready on localhost:${port}" >&2
    return 1
}

echo "🔌 Setting up port-forwards to Sigstore services..." >&2
kubectl port-forward -n rekor-system svc/rekor-server "${REKOR_PORT}:80" &>/dev/null &
PORT_FORWARD_PIDS+=($!)
kubectl port-forward -n tuf-system svc/tuf-server "${TUF_PORT}:80" &>/dev/null &
PORT_FORWARD_PIDS+=($!)

wait_for_port "${REKOR_PORT}" "Rekor"
wait_for_port "${TUF_PORT}"   "TUF"

# ---------- TUF init ----------

TUF_ROOT="$(mktemp -d)"
export TUF_ROOT
echo "📦 Initializing cosign TUF root..." >&2
cosign initialize --mirror="${TUF_LOCAL_URL}" --root="${TUF_LOCAL_URL}/root.json"

# ---------- common args ----------

COMMON_ARGS=(
    --rekor-url="${REKOR_LOCAL_URL}"
    --certificate-identity-regexp="${CERTIFICATE_IDENTITY_REGEXP}"
    --certificate-oidc-issuer="${CERTIFICATE_OIDC_ISSUER}"
)
if [ "${INSECURE_REGISTRY}" = "true" ]; then
    COMMON_ARGS+=(--allow-insecure-registry)
fi

# ---------- 1. verify image signature ----------

echo "" >&2
echo "🔍 [1/3] Verifying image signature: ${IMAGE_REF}" >&2
if cosign verify "${COMMON_ARGS[@]}" "${IMAGE_REF}" > /dev/null; then
    echo "✅ Image signature OK" >&2
else
    echo "❌ Image signature verification FAILED" >&2
    exit 1
fi

# ---------- 2. verify SBOM attestation ----------

echo "" >&2
echo "🔍 [2/3] Verifying SBOM attestation: ${IMAGE_REF}" >&2
if cosign verify-attestation --type=spdxjson "${COMMON_ARGS[@]}" "${IMAGE_REF}" > /dev/null; then
    echo "✅ SBOM attestation OK" >&2
else
    echo "❌ SBOM attestation verification FAILED" >&2
    exit 1
fi

# ---------- 3. verify SLSA provenance attestation (Tekton Chains) ----------

CHAINS_ARGS=(
    --rekor-url="${REKOR_LOCAL_URL}"
    --certificate-identity="${CHAINS_IDENTITY}"
    --certificate-oidc-issuer="${CERTIFICATE_OIDC_ISSUER}"
)
if [ "${INSECURE_REGISTRY}" = "true" ]; then
    CHAINS_ARGS+=(--allow-insecure-registry)
fi

echo "" >&2
echo "🔍 [3/3] Verifying SLSA provenance attestation: ${IMAGE_REF}" >&2
if cosign verify-attestation --type=slsaprovenance "${CHAINS_ARGS[@]}" "${IMAGE_REF}" > /dev/null; then
    echo "✅ SLSA provenance attestation OK" >&2
else
    echo "❌ SLSA provenance attestation verification FAILED" >&2
    exit 1
fi

echo "" >&2
echo "✅ All verifications passed for ${IMAGE_REF}" >&2
