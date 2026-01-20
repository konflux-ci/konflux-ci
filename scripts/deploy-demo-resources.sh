#!/bin/bash -eu

# Deploy Demo Resources for Konflux
#
# This script deploys demo users, namespaces, and RBAC configuration for testing
# Konflux functionality. It configures Dex with static demo users.
#
# IMPORTANT: This script works on ANY Kubernetes cluster with Konflux installed,
# not just Kind clusters. The demo resources are for TESTING ONLY.
#
# WARNING: Demo users have hardcoded passwords and should NEVER be used in
# production environments. They are intended for local development and testing.
#
# Default demo credentials:
# - user1@konflux.dev / password
# - user2@konflux.dev / password
#
# Prerequisites:
# - Konflux must be deployed on the cluster
# - kubectl must be configured to access the cluster
# - Dex must be running in the 'dex' namespace
#
# Usage:
#   ./scripts/deploy-demo-resources.sh

# Determine the absolute path of the repository root
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

echo "========================================="
echo "Deploying Konflux Demo Resources"
echo "========================================="
echo ""
echo "WARNING: This deploys INSECURE demo users for testing only!"
echo "Do NOT use these resources in production environments."
echo ""

# Check if Konflux is deployed
if ! kubectl get namespace dex &> /dev/null; then
    echo "ERROR: Dex namespace not found. Is Konflux deployed?"
    echo "Please deploy Konflux before running this script."
    exit 1
fi

# Check if Dex deployment exists
if ! kubectl get deployment dex -n dex &> /dev/null; then
    echo "ERROR: Dex deployment not found in 'dex' namespace."
    echo "Please ensure Konflux is fully deployed before adding demo resources."
    exit 1
fi

echo "✓ Konflux detected - proceeding with demo resource deployment"
echo ""

# Deploy demo user namespaces and RBAC
echo "👥 Deploying demo user namespaces and RBAC..."
kubectl apply -k "${REPO_ROOT}/test/resources/demo-users/user/"

# Deploy Dex configuration with demo users
echo "🔐 Configuring Dex with demo credentials..."
kubectl apply -f "${REPO_ROOT}/test/resources/demo-users/dex-users.yaml"

# Patch Dex deployment to use the demo ConfigMap
echo "🔧 Patching Dex deployment to use demo configmap..."
kubectl patch deployment dex -n dex --type=json \
    -p='[{"op": "replace", "path": "/spec/template/spec/volumes/0/configMap/name", "value": "dex"}]'

# Wait for Dex to restart
echo "⏳ Waiting for Dex to restart with demo users..."
kubectl rollout status deployment/dex -n dex --timeout=120s

echo ""
echo "========================================="
echo "✅ Demo resources deployed successfully!"
echo "========================================="
echo ""
echo "Demo user credentials:"
echo "  - user1@konflux.dev / password"
echo "  - user2@konflux.dev / password"
echo ""
echo "Access Konflux UI at: https://localhost:9443"
echo "(Port may differ based on your ingress configuration)"
echo ""
echo "REMEMBER: These are insecure demo credentials for testing only!"
