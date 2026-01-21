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
# - Konflux must be deployed on the cluster (operator-based or bootstrap)
# - kubectl must be configured to access the cluster
#
# Usage:
#   ./scripts/deploy-demo-resources.sh
#
# See docs/demo-users.md for more information about demo user configuration.

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

# Detect deployment type (operator-based or bootstrap)
DEPLOYMENT_TYPE=""
if kubectl get konfluxui konflux-ui -n konflux-ui &> /dev/null; then
    DEPLOYMENT_TYPE="operator"
    echo "✓ Detected operator-based Konflux deployment"
elif kubectl get namespace dex &> /dev/null && kubectl get deployment dex -n dex &> /dev/null; then
    DEPLOYMENT_TYPE="bootstrap"
    echo "✓ Detected bootstrap Konflux deployment"
else
    echo "ERROR: Konflux deployment not detected."
    echo ""
    echo "Could not find either:"
    echo "  - Operator deployment: KonfluxUI CR in konflux-ui namespace"
    echo "  - Bootstrap deployment: Dex deployment in dex namespace"
    echo ""
    echo "Please deploy Konflux before running this script."
    exit 1
fi

echo ""

# Deploy demo user namespaces and RBAC
echo "👥 Deploying demo user namespaces and RBAC..."
kubectl apply -k "${REPO_ROOT}/test/resources/demo-users/user/"

# Configure demo users based on deployment type
if [ "$DEPLOYMENT_TYPE" = "operator" ]; then
    echo "🔐 Configuring demo users via KonfluxUI CR..."

    # Patch the KonfluxUI CR to add static passwords
    kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      enablePasswordDB: true
      staticPasswords:
      - email: "user1@konflux.dev"
        username: "user1"
        userID: "7138d2fe-724e-4e86-af8a-db7c4b080e20"
        hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
      - email: "user2@konflux.dev"
        username: "user2"
        userID: "ea8e8ee1-2283-4e03-83d4-b00f8b821b64"
        hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
'

    # Wait for Dex to restart with new configuration
    echo "⏳ Waiting for Dex to restart with demo users..."
    kubectl rollout status deployment/dex -n konflux-ui --timeout=120s

else
    # Bootstrap deployment - use ConfigMap approach
    echo "🔐 Configuring Dex with demo credentials (bootstrap method)..."
    kubectl apply -f "${REPO_ROOT}/test/resources/demo-users/dex-users.yaml"

    # Patch Dex deployment to use the demo ConfigMap
    echo "🔧 Patching Dex deployment to use demo configmap..."
    kubectl patch deployment dex -n dex --type=json \
        -p='[{"op": "replace", "path": "/spec/template/spec/volumes/0/configMap/name", "value": "dex"}]'

    # Wait for Dex to restart
    echo "⏳ Waiting for Dex to restart with demo users..."
    kubectl rollout status deployment/dex -n dex --timeout=120s
fi

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
echo ""
if [ "$DEPLOYMENT_TYPE" = "operator" ]; then
    echo "Note: Demo users are configured via the KonfluxUI custom resource."
    echo "To remove them, patch the KonfluxUI CR to remove staticPasswords."
    echo "See docs/demo-users.md for details."
fi
