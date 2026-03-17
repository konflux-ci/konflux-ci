#!/bin/bash -e
#
# Deploy Konflux on OpenShift
#
# This script deploys Konflux using the operator-based approach on OCP.
# It uses OpenShift Pipelines (Red Hat's productized Tekton) instead of
# upstream Tekton Operator for native OCP compatibility.
#
# Prerequisites:
#   - oc/kubectl configured with cluster access
#   - Running from the root of the konflux-ci repository
#   - OpenShift cluster with OperatorHub access
#
# Environment variables (optional):
#   OPERATOR_IMAGE        - Full operator image to use (skips SHA-based construction)
#   KONFLUX_OPERATOR_REPO - Operator image repository (default: quay.io/redhat-user-workloads/konflux-vanguard-tenant/konflux-operator)
#
# Environment variables (set by OpenShift CI/Prow):
#   REPO_NAME      - Name of the repository being tested (e.g., "konflux-ci", "release")
#   PULL_PULL_SHA  - Git SHA of the PR head commit being tested
#

set -o nounset
set -o errexit
set -o pipefail

# Determine the absolute path of the repository root
REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)

# On OCP, use 'oc' as kubectl if kubectl is not available
# oc is a superset of kubectl and works for all kubectl commands
if ! command -v kubectl &>/dev/null && command -v oc &>/dev/null; then
    echo "kubectl not found, using 'oc' as kubectl"
    function kubectl() { oc "$@"; }
    export -f kubectl
fi

echo "=== Installing Konflux on OpenShift ==="

# Debug: Show git information
echo "Current branch: $(git branch --show-current)"
echo "Current commit: $(git rev-parse HEAD)"
echo "Git log:"
git --no-pager log --oneline -3
echo ""

# Configuration
KONFLUX_OPERATOR_REPO="${KONFLUX_OPERATOR_REPO:-quay.io/redhat-user-workloads/konflux-vanguard-tenant/konflux-operator}"

# Determine operator image to use
if [[ -n "${OPERATOR_IMAGE:-}" ]]; then
    echo "Using provided OPERATOR_IMAGE: ${OPERATOR_IMAGE}"
else
    # Only use PULL_PULL_SHA if this is actually a PR to konflux-ci (not rehearsal jobs)
    if [[ "${REPO_NAME:-}" == "konflux-ci" && -n "${PULL_PULL_SHA:-}" ]]; then
        COMMIT_SHA="${PULL_PULL_SHA}"
        OPERATOR_IMAGE="${KONFLUX_OPERATOR_REPO}:on-pr-${COMMIT_SHA}"
        echo "Using PR image: ${OPERATOR_IMAGE}"
    else
        # Rehearsal, periodic, or local testing - use latest tag
        OPERATOR_IMAGE="quay.io/konflux-ci/konflux-operator:latest"
        echo "Using fallback image: ${OPERATOR_IMAGE}"
    fi
fi

# Wait for operator image to be available (Konflux CI may still be building it)
echo "Checking if operator image is available..."
MAX_WAIT=900  # 15 minutes
WAIT_INTERVAL=30
WAITED=0
# Use --filter-by-os for multi-arch images
while ! oc image info --filter-by-os=linux/amd64 "${OPERATOR_IMAGE}" &>/dev/null; do
    if [[ $WAITED -ge $MAX_WAIT ]]; then
        echo "ERROR: Operator image not available after ${MAX_WAIT}s: ${OPERATOR_IMAGE}"
        exit 1
    fi
    echo "  Image not ready, waiting ${WAIT_INTERVAL}s... (${WAITED}/${MAX_WAIT}s)"
    sleep $WAIT_INTERVAL
    WAITED=$((WAITED + WAIT_INTERVAL))
done
echo "Operator image is available!"

# Step 1: Deploy dependencies
# - USE_OPENSHIFT_PIPELINES: Use OCP's native Tekton instead of upstream
# - USE_OPENSHIFT_CERTMANAGER: Use Red Hat cert-manager operator instead of upstream
# - SKIP_INTERNAL_REGISTRY: OCP has its own registry
# - SKIP_DEX: OCP has its own OAuth/authentication
# - SKIP_SMEE: Skip Smee when no channel is configured
echo ""
echo "=== Step 1/6: Deploying dependencies ==="

# Pre-configure Smee channel if specified
if [ -n "${SMEE_CHANNEL:-}" ]; then
    echo "Configuring Smee channel: ${SMEE_CHANNEL}"
    SMEE_DIR="${REPO_ROOT}/dependencies/smee"
    sed "s|https://smee.io/CHANNELID|${SMEE_CHANNEL}|g" \
        "${SMEE_DIR}/smee-channel-id.tpl" \
        > "${SMEE_DIR}/smee-channel-id.yaml"
    SKIP_SMEE=false
else
    SKIP_SMEE=true
fi

USE_OPENSHIFT_PIPELINES=true \
USE_OPENSHIFT_CERTMANAGER=true \
SKIP_INTERNAL_REGISTRY=true \
SKIP_DEX=true \
SKIP_SMEE="${SKIP_SMEE}" \
"${REPO_ROOT}/deploy-deps.sh"

# Step 2: Install CRDs from the checked-out branch
echo ""
echo "=== Step 2/6: Installing Operator CRDs ==="
cd operator
# Clear GOFLAGS to allow downloading tools (CI may have -mod=vendor set)
GOFLAGS="" make install

# Step 3: Deploy the operator using the Konflux-built image
echo ""
echo "=== Step 3/6: Deploying Operator ==="
echo "Image: ${OPERATOR_IMAGE}"
# GOFLAGS="" needed because CI sets -mod=vendor which blocks kustomize download
GOFLAGS="" make deploy IMG="${OPERATOR_IMAGE}"

# Step 4: Wait for the operator deployment to be available
echo ""
echo "=== Step 4/6: Waiting for Operator to be ready ==="
oc wait --for=condition=Available deployment/konflux-operator-controller-manager \
    -n konflux-operator --timeout=300s
echo "Operator is ready!"

# Step 5: Create Konflux CR instance
echo ""
echo "=== Step 5/6: Creating Konflux CR ==="
KONFLUX_CR=$("${REPO_ROOT}/scripts/resolve-konflux-cr.sh")
echo "Applying: ${KONFLUX_CR}"
oc apply -f "${KONFLUX_CR}"

# Step 6: Deploy secrets
echo ""
echo "=== Step 6/6: Deploying secrets ==="
USE_OPENSHIFT_PIPELINES=true "${REPO_ROOT}/scripts/deploy-secrets.sh"

# Wait for Konflux to be fully ready
echo ""
echo "=== Waiting for Konflux to be ready ==="
oc wait --for=condition=Ready=True konflux konflux --timeout=600s

echo ""
echo "============================================"
echo "  Konflux installation complete!"
echo "============================================"
