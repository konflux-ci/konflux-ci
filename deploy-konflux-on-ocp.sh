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
#   KONFLUX_OPERATOR_REGISTRY - Registry for operator images (default: quay.io/redhat-user-workloads/konflux-vanguard-tenant)
#

set -o nounset
set -o errexit
set -o pipefail

echo "=== Installing Konflux on OpenShift ==="

# Configuration
KONFLUX_OPERATOR_REGISTRY="${KONFLUX_OPERATOR_REGISTRY:-quay.io/redhat-user-workloads/konflux-vanguard-tenant}"

# Get the script's directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

# Determine operator image to use
if [[ -n "${OPERATOR_IMAGE:-}" ]]; then
    echo "Using provided OPERATOR_IMAGE: ${OPERATOR_IMAGE}"
else
    # Construct image tag from commit SHA
    COMMIT_SHA=$(git rev-parse HEAD)
    OPERATOR_IMAGE="${KONFLUX_OPERATOR_REGISTRY}/konflux-operator:on-pr-${COMMIT_SHA}"
    echo "Commit SHA: ${COMMIT_SHA}"
    echo "Operator image: ${OPERATOR_IMAGE}"
fi

# Step 1: Install OpenShift Pipelines Operator
echo ""
echo "=== Step 1/8: Installing OpenShift Pipelines Operator ==="
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: openshift-pipelines-operator
  namespace: openshift-operators
spec:
  channel: latest
  name: openshift-pipelines-operator-rh
  source: redhat-operators
  sourceNamespace: openshift-marketplace
EOF

# Step 2: Wait for OpenShift Pipelines to be ready
echo ""
echo "=== Step 2/8: Waiting for OpenShift Pipelines to be ready ==="
echo "Waiting for TektonConfig CRD to be available..."
until oc get crd tektonconfigs.operator.tekton.dev &>/dev/null; do
    echo "  Waiting for TektonConfig CRD..."
    sleep 10
done

echo "Waiting for TektonConfig to be ready..."
until oc get tektonconfig config &>/dev/null; do
    echo "  Waiting for TektonConfig resource..."
    sleep 10
done
oc wait --for=condition=Ready tektonconfig/config --timeout=600s

# Wait for Tekton webhook services to be available (TektonConfig Ready doesn't guarantee this)
echo "Waiting for Tekton webhook services..."
until oc get service tekton-operator-proxy-webhook -n openshift-pipelines &>/dev/null; do
    echo "  Waiting for tekton-operator-proxy-webhook service..."
    sleep 10
done
oc wait --for=condition=Available deployment/tekton-operator-proxy-webhook -n openshift-pipelines --timeout=120s
echo "OpenShift Pipelines is ready!"

# Step 3: Deploy other dependencies
# - SKIP_TEKTON: Using OpenShift Pipelines instead
# - SKIP_INTERNAL_REGISTRY: OCP has its own registry
# - SKIP_DEX: OCP has its own OAuth/authentication
# - SKIP_SMEE: Webhook relay not needed for CI testing
echo ""
echo "=== Step 3/8: Deploying other dependencies ==="
SKIP_TEKTON=true SKIP_INTERNAL_REGISTRY=true SKIP_DEX=true SKIP_SMEE=true ./deploy-deps.sh

# Step 4: Install CRDs from the checked-out branch
echo ""
echo "=== Step 4/8: Installing Operator CRDs ==="
cd operator
make install

# Step 5: Deploy the operator using the Konflux-built image
echo ""
echo "=== Step 5/8: Deploying Operator ==="
echo "Image: ${OPERATOR_IMAGE}"
make deploy IMG="${OPERATOR_IMAGE}"

# Step 6: Wait for the operator deployment to be available
echo ""
echo "=== Step 6/8: Waiting for Operator to be ready ==="
oc wait --for=condition=Available deployment/konflux-operator-controller-manager \
    -n konflux-operator --timeout=300s
echo "Operator is ready!"

# Step 7: Fix operator RBAC permissions for OCP
# The operator needs 'delete' permission on RBAC resources to set ownerReferences
# This is required for UI and info components to work correctly
echo ""
echo "=== Step 7/8: Patching operator RBAC for OCP compatibility ==="
oc patch clusterrole konflux-operator-manager-role --type='json' -p='[
  {"op": "add", "path": "/rules/-", "value": {"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["clusterroles", "clusterrolebindings", "roles", "rolebindings"], "verbs": ["delete"]}}
]'
echo "RBAC patched. Restarting operator to pick up new permissions..."
oc rollout restart deployment/konflux-operator-controller-manager -n konflux-operator
oc rollout status deployment/konflux-operator-controller-manager -n konflux-operator --timeout=120s

# Step 8: Create Konflux CR instance
echo ""
echo "=== Step 8/8: Creating Konflux CR ==="
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
