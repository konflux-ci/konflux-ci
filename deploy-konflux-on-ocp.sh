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
#   OPERATOR_IMAGE            - Full operator image to use (skips SHA-based construction)
#   KONFLUX_OPERATOR_REPO - Operator image repository (default: quay.io/redhat-user-workloads/konflux-vanguard-tenant/konflux-operator)
#

set -o nounset
set -o errexit
set -o pipefail

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
git log --oneline -3
echo ""

# Configuration
KONFLUX_OPERATOR_REPO="${KONFLUX_OPERATOR_REPO:-quay.io/redhat-user-workloads/konflux-vanguard-tenant/konflux-operator}"

# Get the script's directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

# Determine operator image to use
if [[ -n "${OPERATOR_IMAGE:-}" ]]; then
    echo "Using provided OPERATOR_IMAGE: ${OPERATOR_IMAGE}"
else
    # Construct image tag from commit SHA
    # In CI (Prow), use PULL_PULL_SHA (PR head) instead of HEAD (merge commit)
    if [[ -n "${PULL_PULL_SHA:-}" ]]; then
        COMMIT_SHA="${PULL_PULL_SHA}"
        echo "Using PR head SHA (PULL_PULL_SHA): ${COMMIT_SHA}"
    else
        COMMIT_SHA=$(git rev-parse HEAD)
        echo "Using git HEAD SHA: ${COMMIT_SHA}"
    fi
    OPERATOR_IMAGE="${KONFLUX_OPERATOR_REPO}:on-pr-${COMMIT_SHA}"
    echo "Operator image: ${OPERATOR_IMAGE}"
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
# - SKIP_INTERNAL_REGISTRY: OCP has its own registry
# - SKIP_DEX: OCP has its own OAuth/authentication
# - SKIP_SMEE: Webhook relay not needed for CI testing
echo ""
echo "=== Step 1/6: Deploying dependencies ==="
USE_OPENSHIFT_PIPELINES=true SKIP_INTERNAL_REGISTRY=true SKIP_DEX=true SKIP_SMEE=true ./deploy-deps.sh

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
GOFLAGS="" make deploy IMG="${OPERATOR_IMAGE}"

# Step 4: Wait for the operator deployment to be available
echo ""
echo "=== Step 4/6: Waiting for Operator to be ready ==="
oc wait --for=condition=Available deployment/konflux-operator-controller-manager \
    -n konflux-operator --timeout=300s
echo "Operator is ready!"

# Step 5: Fix operator RBAC permissions for OCP
# The operator needs 'delete' permission on RBAC resources to set ownerReferences
# This is required for UI and info components to work correctly
echo ""
echo "=== Step 5/6: Patching operator RBAC for OCP compatibility ==="
oc patch clusterrole konflux-operator-manager-role --type='json' -p='[
  {"op": "add", "path": "/rules/-", "value": {"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["clusterroles", "clusterrolebindings", "roles", "rolebindings"], "verbs": ["delete"]}}
]'
echo "RBAC patched. Restarting operator to pick up new permissions..."
oc rollout restart deployment/konflux-operator-controller-manager -n konflux-operator
oc rollout status deployment/konflux-operator-controller-manager -n konflux-operator --timeout=120s

# Step 6: Create Konflux CR instance
echo ""
echo "=== Step 6/6: Creating Konflux CR ==="
oc apply -f config/samples/konflux_v1alpha1_konflux.yaml

# Wait for Konflux to be fully ready
echo ""
echo "=== Waiting for Konflux to be ready ==="
cd "${SCRIPT_DIR}"
oc wait --for=condition=Ready=True konflux konflux --timeout=600s

echo ""
echo "============================================"
echo "  Konflux installation complete!"
echo "============================================"
