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
#   none              - Skip operator install and Konflux CR (for running operator locally)
#
# NOTE: The 'local' method applies manifests from your checkout with the latest
# released image, which may cause mismatches if your checkout differs from the
# release. To avoid this, checkout a specific release tag first:
#   git checkout v1.0.0  # or the desired release tag
#   OPERATOR_INSTALL_METHOD=local ./scripts/deploy-local.sh
#
# For 'none' method, the script sets up Kind + dependencies + secrets, then exits.
# You then run the operator yourself:
#   cd operator && make install && make run

set -euo pipefail

# Determine the absolute path of the repository root
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

# Optional: Load environment configuration from file if it exists.
# Precedence (high to low): injected env vars > env file > script defaults.
# Snapshot the caller's environment first so that any vars passed on the
# command line (e.g. OPERATOR_INSTALL_METHOD=none ./scripts/deploy-local.sh)
# are restored after sourcing and therefore take priority over the env file.
ENV_FILE="${SCRIPT_DIR}/deploy-local.env"
if [ -f "${ENV_FILE}" ]; then
    echo "Loading configuration from ${ENV_FILE}"
    _pre_env=$(export -p)
    # shellcheck disable=SC1090
    source "${ENV_FILE}"
    eval "$_pre_env"
    unset _pre_env
fi

# Optional variables with defaults (using :- pattern)
KIND_CLUSTER="${KIND_CLUSTER:-konflux}"
KIND_MEMORY_GB="${KIND_MEMORY_GB:-8}"
REGISTRY_HOST_PORT="${REGISTRY_HOST_PORT:-5001}"
ENABLE_REGISTRY_PORT="${ENABLE_REGISTRY_PORT:-1}"
INCREASE_PODMAN_PIDS_LIMIT="${INCREASE_PODMAN_PIDS_LIMIT:-1}"
ENABLE_IMAGE_CACHE="${ENABLE_IMAGE_CACHE:-0}"
OPERATOR_INSTALL_METHOD="${OPERATOR_INSTALL_METHOD:-release}"
OPERATOR_IMAGE="${OPERATOR_IMAGE:-quay.io/konflux-ci/konflux-operator:latest}"

# Export variables for child scripts
export KIND_CLUSTER KIND_MEMORY_GB PODMAN_MACHINE_NAME REGISTRY_HOST_PORT ENABLE_REGISTRY_PORT
export INCREASE_PODMAN_PIDS_LIMIT ENABLE_IMAGE_CACHE
export GITHUB_PRIVATE_KEY GITHUB_PRIVATE_KEY_PATH GITHUB_APP_ID WEBHOOK_SECRET QUAY_TOKEN QUAY_ORGANIZATION QUAY_API_URL
export SEGMENT_WRITE_KEY

# Child scripts only see exported variables (values from deploy-local.env are not
# exported by sourcing); validate secrets after exports so checks see the same env.
VALIDATE_ONLY=true "${SCRIPT_DIR}/deploy-secrets.sh"

# Get Konflux CR file path (command-line arg takes highest precedence)
KONFLUX_CR="${1:-${KONFLUX_CR:-}}"
export KONFLUX_CR

# Resolve CR using shared logic (auto-selects e2e CR when Quay credentials are set)
KONFLUX_CR=$("${SCRIPT_DIR}/resolve-konflux-cr.sh")

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

# Step 1: Setup Kind cluster (skip when using an existing kubeconfig, e.g. Tekton kind-aws-provision)
if [ "${DEPLOY_LOCAL_SKIP_KIND:-0}" = "1" ]; then
    echo "========================================="
    echo "Step 1: Skipped (DEPLOY_LOCAL_SKIP_KIND=1 — using current KUBECONFIG)"
    echo "========================================="
else
    echo "========================================="
    echo "Step 1: Creating Kind cluster"
    echo "========================================="
    "${SCRIPT_DIR}/setup-kind-local-cluster.sh"
fi

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

    none)
        echo "Skipping operator installation (OPERATOR_INSTALL_METHOD=none)"
        echo "You will need to run the operator manually after deployment completes:"
        echo "  cd operator && make install && make run"
        ;;

    *)
        echo "ERROR: Invalid OPERATOR_INSTALL_METHOD: ${INSTALL_METHOD}"
        echo "Valid options: local, build, release, none"
        exit 1
        ;;
esac

if [ "${INSTALL_METHOD}" != "none" ]; then
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
else
    echo ""
    echo "========================================="
    echo "Steps 4-5: Skipped (operator not installed)"
    echo "========================================="
fi

# Step 6: Create secrets for GitHub integration and optional image-controller
echo ""
echo "========================================="
echo "Step 6: Setting up secrets"
echo "========================================="

# In 'none' mode, namespaces don't exist yet (operator isn't running),
# so create them directly and skip waiting for pods that don't exist yet.
if [ "${INSTALL_METHOD}" = "none" ]; then
    env CREATE_NAMESPACES=true WAIT_FOR_PODS=false "${SCRIPT_DIR}/deploy-secrets.sh"
else
    "${SCRIPT_DIR}/deploy-secrets.sh"
fi

echo "✓ Secrets created"

if [ "${INSTALL_METHOD}" != "none" ]; then
    # Step 7: Wait for Konflux to be ready
    echo ""
    echo "========================================="
    echo "Step 7: Waiting for Konflux to be ready"
    echo "========================================="
    echo "This may take several minutes..."

    if ! kubectl wait --for=condition=Ready=True konflux konflux --timeout=15m 2>/dev/null; then
        echo ""
        echo "ERROR: Konflux CR did not become Ready within 15 minutes"
        echo ""
        echo "Debug with:"
        echo "  kubectl get konflux konflux -o yaml"
        echo "  kubectl get konflux konflux -o jsonpath='{.status.conditions}'"
        exit 1
    fi
    echo "✓ Konflux is ready"
else
    echo ""
    echo "========================================="
    echo "Step 7: Skipped (operator not installed)"
    echo "========================================="
fi

# Final status
echo ""
echo "========================================="
echo "✅ Deployment Complete!"
echo "========================================="
echo ""

if [ "${INSTALL_METHOD}" = "none" ]; then
    echo "Kind cluster and dependencies are ready."
    echo ""
    echo "Next steps - run the operator:"
    echo "  cd operator"
    echo "  make install   # Install CRDs"
    echo "  make run       # Run the operator locally"
    echo ""
    echo "Then, in another terminal, apply the Konflux CR:"
    echo "  kubectl apply -f ${KONFLUX_CR}"
    echo ""
else
    echo "Konflux is now running on your local Kind cluster"
    echo ""
    echo "Access the UI:"
    echo "  https://localhost:9443"
    echo ""

    echo "Demo user credentials:"
    echo "  user1@konflux.dev / password"
    echo "  user2@konflux.dev / password"
    echo ""
fi

if [[ "${ENABLE_REGISTRY_PORT:-1}" -eq 1 ]]; then
    echo "Internal registry:"
    echo "  localhost:${REGISTRY_HOST_PORT:-5001}"
    echo ""
fi
