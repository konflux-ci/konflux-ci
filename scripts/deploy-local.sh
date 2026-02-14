#!/usr/bin/env bash

# Deploy Konflux for Local Development
#
# This script provides a one-command local development deployment of Konflux
# on a Kind cluster. It's designed for LOCAL DEVELOPMENT CONVENIENCE ONLY.
#
# For production deployments on real clusters, see docs/operator-deployment.md
#
# What this script does:
#  1. Creates a Kind cluster with proper configuration
#  2. Deploys the Konflux operator
#  3. Applies a Konflux CR configuration
#  4. Creates secrets for GitHub integration
#
# Prerequisites:
#  - kind, kubectl, podman (or docker)
#  - kustomize (only for 'local' install method)
#  - Configuration file: scripts/deploy-local.env
#
# Usage:
#   ./scripts/deploy-local.sh [konflux-cr-file]
#
# By default, uses operator/config/samples/konflux_v1alpha1_konflux.yaml
#
# Example:
#   cp scripts/deploy-local.env.template scripts/deploy-local.env
#   # Edit deploy-local.env with your secrets
#   ./scripts/deploy-local.sh
#
# To customize the Konflux configuration:
#   cp operator/config/samples/konflux_v1alpha1_konflux.yaml my-konflux.yaml
#   # Edit my-konflux.yaml as needed
#   ./scripts/deploy-local.sh my-konflux.yaml
#
# Operator Installation Methods (OPERATOR_INSTALL_METHOD):
#   release (default) - Install from latest GitHub release
#   local             - Install from current checkout using kustomize (see note below)
#   build             - Build operator image locally and install (for operator developers)
#
# NOTE: The 'local' method applies manifests from your checkout with the latest
# released image, which may cause mismatches if your checkout differs from the
# release. To avoid this, checkout a specific release tag first:
#   git checkout v1.0.0  # or the desired release tag
#   OPERATOR_INSTALL_METHOD=local ./scripts/deploy-local.sh

set -euo pipefail

# Determine the absolute path of the repository root
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

# Optional: Load environment configuration from file if it exists
ENV_FILE="${SCRIPT_DIR}/deploy-local.env"
if [ -f "${ENV_FILE}" ]; then
    echo "Loading configuration from ${ENV_FILE}"
    # shellcheck disable=SC1090
    source "${ENV_FILE}"
fi

# Validate REQUIRED variables
GITHUB_APP_ID="${GITHUB_APP_ID:?GitHub App ID is required. Set GITHUB_APP_ID}"
WEBHOOK_SECRET="${WEBHOOK_SECRET:?Webhook secret is required. Set WEBHOOK_SECRET}"

# Validate that at least one private key option is provided
if [ -z "${GITHUB_PRIVATE_KEY:-}" ] && [ -z "${GITHUB_PRIVATE_KEY_PATH:-}" ]; then
    echo "ERROR: GitHub private key is required" >&2
    echo "" >&2
    echo "Set one of:" >&2
    echo "  - GITHUB_PRIVATE_KEY (literal key content)" >&2
    echo "  - GITHUB_PRIVATE_KEY_PATH (path to .pem file)" >&2
    exit 1
fi

# Validate private key file exists if path is provided
if [ -n "${GITHUB_PRIVATE_KEY_PATH:-}" ] && [ ! -f "${GITHUB_PRIVATE_KEY_PATH}" ]; then
    echo "ERROR: GitHub private key file not found: ${GITHUB_PRIVATE_KEY_PATH}" >&2
    exit 1
fi

# Optional variables with defaults (using :- pattern)
KIND_CLUSTER="${KIND_CLUSTER:-konflux}"
KIND_MEMORY_GB="${KIND_MEMORY_GB:-8}"
REGISTRY_HOST_PORT="${REGISTRY_HOST_PORT:-5001}"
ENABLE_REGISTRY_PORT="${ENABLE_REGISTRY_PORT:-1}"
INCREASE_PODMAN_PIDS_LIMIT="${INCREASE_PODMAN_PIDS_LIMIT:-1}"
OPERATOR_INSTALL_METHOD="${OPERATOR_INSTALL_METHOD:-release}"
OPERATOR_IMAGE="${OPERATOR_IMAGE:-quay.io/konflux-ci/konflux-operator:latest}"

# Export variables for child scripts
export KIND_CLUSTER KIND_MEMORY_GB PODMAN_MACHINE_NAME REGISTRY_HOST_PORT ENABLE_REGISTRY_PORT
export INCREASE_PODMAN_PIDS_LIMIT
export GITHUB_PRIVATE_KEY GITHUB_APP_ID WEBHOOK_SECRET QUAY_TOKEN QUAY_ORGANIZATION

# Get Konflux CR file path (precedence, high->low: command-line arg, env var, default)
KONFLUX_CR="${1:-$KONFLUX_CR}"

# Auto-select e2e CR when Quay credentials are configured but no explicit CR specified
if [ -n "${QUAY_TOKEN:-}" ] && [ -n "${QUAY_ORGANIZATION:-}" ] && [ -z "${KONFLUX_CR}" ]; then
    KONFLUX_CR="${REPO_ROOT}/operator/config/samples/konflux-e2e.yaml"
    echo ""
    echo "INFO: Auto-selecting konflux-e2e.yaml because QUAY_TOKEN/QUAY_ORGANIZATION are set"
    echo "      This CR enables image-controller required for Quay integration"
    echo "      To use a different CR, set KONFLUX_CR environment variable or pass as argument"
    echo ""
else
    KONFLUX_CR="${KONFLUX_CR:-${REPO_ROOT}/operator/config/samples/konflux_v1alpha1_konflux.yaml}"
fi

# Convert relative path to absolute (if not already absolute)
if [[ "${KONFLUX_CR}" != /* ]]; then
    KONFLUX_CR="${REPO_ROOT}/${KONFLUX_CR}"
fi

if [ ! -f "${KONFLUX_CR}" ]; then
    echo "ERROR: Konflux CR file not found: ${KONFLUX_CR}"
    echo ""
    echo "Usage: $0 [konflux-cr-file]"
    exit 1
fi

echo "========================================="
echo "Konflux Local Development Deployment"
echo "========================================="
echo ""
echo "Configuration:"
echo "  Environment: ${ENV_FILE}"
echo "  Konflux CR:  ${KONFLUX_CR}"
echo ""

INSTALL_METHOD="${OPERATOR_INSTALL_METHOD:-local}"

# For 'build' method, build the operator image before creating the cluster to reduce peak memory (no Kind container during go build)
if [ "${INSTALL_METHOD}" = "build" ]; then
    echo "========================================="
    echo "Building operator image (before cluster)"
    echo "========================================="
    cd "${REPO_ROOT}/operator"
    OPERATOR_IMG="localhost/konflux-operator:local"
    make docker-build IMG="${OPERATOR_IMG}"
    cd "${REPO_ROOT}"
    echo ""
fi

# Step 1: Setup Kind cluster
echo "========================================="
echo "Step 1: Creating Kind cluster"
echo "========================================="
"${SCRIPT_DIR}/setup-kind-local-cluster.sh"

# Step 2: Deploy dependencies
echo ""
echo "========================================="
echo "Step 2: Deploying dependencies"
echo "========================================="
echo "Installing Tekton, cert-manager, and other prerequisites..."

# Pre-configure Smee channel if specified (E2E tests or local dev with specific channel)
if [ -n "${SMEE_CHANNEL:-}" ]; then
    echo "Configuring Smee channel: ${SMEE_CHANNEL}"
    SMEE_DIR="${REPO_ROOT}/dependencies/smee"
    sed "s|https://smee.io/CHANNELID|${SMEE_CHANNEL}|g" \
        "${SMEE_DIR}/smee-channel-id.tpl" \
        > "${SMEE_DIR}/smee-channel-id.yaml"
fi

# Skip components managed by the operator
SKIP_DEX=true \
SKIP_KONFLUX_INFO=true \
SKIP_CLUSTER_ISSUER=true \
SKIP_INTERNAL_REGISTRY=true \
"${REPO_ROOT}/deploy-deps.sh"

# Step 3: Deploy Konflux operator
echo ""
echo "========================================="
echo "Step 3: Deploying Konflux operator"
echo "========================================="
echo "Using installation method: ${INSTALL_METHOD}"

case "${INSTALL_METHOD}" in
    local)
        echo "Installing from current commit using kustomize..."
        cd "${REPO_ROOT}/operator"
        OPERATOR_IMG="${OPERATOR_IMAGE:-quay.io/konflux-ci/konflux-operator:latest}"

        make deploy IMG="${OPERATOR_IMG}"

        # Reset kustomization changes to avoid leaving modified files
        git checkout config/manager/kustomization.yaml 2>/dev/null || true
        cd "${REPO_ROOT}"
        ;;

    build)
        echo "Loading operator image into Kind cluster..."
        cd "${REPO_ROOT}/operator"
        kind load docker-image "${OPERATOR_IMG}" --name "${KIND_CLUSTER}"

        echo "Installing CRDs..."
        make install

        echo "Deploying operator..."
        make deploy IMG="${OPERATOR_IMG}"
        cd "${REPO_ROOT}"
        ;;

    release)
        echo "Installing from latest GitHub release..."
        RELEASE_URL="https://github.com/konflux-ci/konflux-ci/releases/latest/download/install.yaml"
        echo "Downloading: ${RELEASE_URL}"
        kubectl apply -f "${RELEASE_URL}"
        ;;

    *)
        echo "ERROR: Invalid OPERATOR_INSTALL_METHOD: ${INSTALL_METHOD}"
        echo "Valid options: local, build, release"
        exit 1
        ;;
esac

# Step 4: Wait for operator to be ready
echo ""
echo "========================================="
echo "Step 4: Waiting for operator"
echo "========================================="
echo "Waiting for operator deployment..."
kubectl wait --for=condition=Available \
    deployment/konflux-operator-controller-manager \
    -n konflux-operator \
    --timeout=5m

echo "✓ Operator is ready"

# Step 5: Apply Konflux CR
echo ""
echo "========================================="
echo "Step 5: Applying Konflux configuration"
echo "========================================="
echo "Applying: ${KONFLUX_CR}"
kubectl apply -f "${KONFLUX_CR}"

# Step 6: Create secrets for GitHub integration
echo ""
echo "========================================="
echo "Step 6: Creating GitHub integration secrets"
echo "========================================="
echo "Creating Pipelines-as-Code secrets..."

# Wait for namespaces to be created by operator
for ns in pipelines-as-code build-service integration-service; do
    echo "Waiting for namespace: ${ns}"
    timeout=60
    while ! kubectl get namespace "${ns}" &> /dev/null && [ $timeout -gt 0 ]; do
        sleep 2
        timeout=$((timeout - 2))
    done

    if [ $timeout -le 0 ]; then
        echo "WARNING: Namespace ${ns} not created after 60 seconds"
        echo "         Secrets will need to be created manually"
        continue
    fi

    echo "Creating secret in ${ns}..."
    # Use different kubectl syntax based on how private key is provided:
    # - File path: use --from-file (local dev with .pem file)
    # - Literal value: use --from-literal (CI with env var, matches prepare-e2e.sh)
    if [ -n "${GITHUB_PRIVATE_KEY_PATH:-}" ] && [ -f "${GITHUB_PRIVATE_KEY_PATH}" ]; then
        kubectl -n "$ns" create secret generic pipelines-as-code-secret \
            --from-file=github-private-key="${GITHUB_PRIVATE_KEY_PATH}" \
            --from-literal github-application-id="$GITHUB_APP_ID" \
            --from-literal webhook.secret="$WEBHOOK_SECRET" \
            --dry-run=client -o yaml | kubectl apply -f -
    else
        kubectl -n "$ns" create secret generic pipelines-as-code-secret \
            --from-literal github-private-key="$GITHUB_PRIVATE_KEY" \
            --from-literal github-application-id="$GITHUB_APP_ID" \
            --from-literal webhook.secret="$WEBHOOK_SECRET" \
            --dry-run=client -o yaml | kubectl apply -f -
    fi
done

echo "✓ Secrets created"

# Step 6b: Create image-controller secret (optional)
if [ -n "${QUAY_TOKEN}" ] && [ -n "${QUAY_ORGANIZATION}" ]; then
    echo ""
    echo "Creating image-controller Quay secret..."

    # Wait for image-controller namespace
    echo "Waiting for namespace: image-controller"
    timeout=60
    while ! kubectl get namespace image-controller &> /dev/null && [ $timeout -gt 0 ]; do
        sleep 2
        timeout=$((timeout - 2))
    done

    if [ $timeout -le 0 ]; then
        echo "WARNING: Namespace image-controller not created after 60 seconds"
        echo "         Secret will need to be created manually"
    else
        echo "Creating secret in image-controller..."
        kubectl -n image-controller create secret generic quaytoken \
            --from-literal=quaytoken="${QUAY_TOKEN}" \
            --from-literal=organization="${QUAY_ORGANIZATION}" \
            --dry-run=client -o yaml | kubectl apply -f -
        echo "✓ Image-controller secret created"

        # Wait for image-controller pods to be ready
        echo "Waiting for image-controller pods to be ready..."
        if kubectl wait --for=condition=Ready --timeout=240s \
            -l control-plane=controller-manager -n image-controller pod 2>/dev/null; then
            echo "✓ Image-controller is ready"
        else
            echo "WARNING: Image-controller pods did not become ready within 4 minutes"
            echo "         This may cause E2E test failures"
        fi
    fi
elif [ -n "${QUAY_TOKEN}" ] || [ -n "${QUAY_ORGANIZATION}" ]; then
    echo ""
    echo "WARNING: Both QUAY_TOKEN and QUAY_ORGANIZATION must be set to create image-controller secret"
    echo "         Image-controller secret not created"
fi

# Step 7: Wait for Konflux to be ready
echo ""
echo "========================================="
echo "Step 7: Waiting for Konflux to be ready"
echo "========================================="
echo "This may take several minutes..."

if ! kubectl wait --for=condition=Ready=True konflux konflux --timeout=15m 2>/dev/null; then
    echo ""
    echo "WARNING: Konflux CR did not become Ready within 15 minutes"
    echo "         This may be normal if deploying all components"
    echo "         Check status with: kubectl get konflux konflux -o yaml"
    echo ""
    echo "To monitor progress:"
    echo "  kubectl get pods -A"
    echo "  kubectl get konflux konflux -o jsonpath='{.status.conditions}'"
else
    echo "✓ Konflux is ready"
fi

# Final status
echo ""
echo "========================================="
echo "✅ Deployment Complete!"
echo "========================================="
echo ""
echo "Konflux is now running on your local Kind cluster"
echo ""
echo "Access the UI:"
echo "  https://localhost:9443"
echo ""

echo "Demo user credentials:"
echo "  user1@konflux.dev / password"
echo "  user2@konflux.dev / password"
echo ""

if [[ "${ENABLE_REGISTRY_PORT:-1}" -eq 1 ]]; then
    echo "Internal registry:"
    echo "  localhost:${REGISTRY_HOST_PORT:-5001}"
    echo ""
fi
