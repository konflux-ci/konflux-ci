#!/bin/bash -eu

# Deploy Konflux for Local Development
#
# This script provides a one-command local development deployment of Konflux
# on a Kind cluster. It's designed for LOCAL DEVELOPMENT CONVENIENCE ONLY.
#
# For production deployments on real clusters, see docs/operator-deployment.md
#
# What this script does:
#  1. Creates a Kind cluster with proper configuration
#  2. Builds and loads the operator image
#  3. Deploys the Konflux operator
#  4. Applies your Konflux CR configuration
#  5. Creates secrets for GitHub integration
#  6. Optionally deploys demo resources (if enabled)
#
# Prerequisites:
#  - kind, kubectl, podman (or docker)
#  - Configuration file: scripts/deploy-local-dev.env
#  - Konflux CR file (default: my-konflux.yaml)
#
# Usage:
#   ./scripts/deploy-local-dev.sh [konflux-cr-file]
#
# Example:
#   cp scripts/deploy-local-dev.env.template scripts/deploy-local-dev.env
#   # Edit deploy-local-dev.env with your secrets
#   cp my-konflux.yaml.template my-konflux.yaml
#   # Edit my-konflux.yaml with your configuration
#   ./scripts/deploy-local-dev.sh my-konflux.yaml

set -o pipefail

# Determine the absolute path of the repository root
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

# Check for environment file
ENV_FILE="${SCRIPT_DIR}/deploy-local-dev.env"
if [ ! -f "${ENV_FILE}" ]; then
    echo "ERROR: Configuration file not found: ${ENV_FILE}"
    echo ""
    echo "Please create it from the template:"
    echo "  cp scripts/deploy-local-dev.env.template scripts/deploy-local-dev.env"
    echo ""
    echo "Then edit scripts/deploy-local-dev.env and fill in your secrets."
    exit 1
fi

# Load environment configuration
# shellcheck disable=SC1090
source "${ENV_FILE}"

# Validate required secrets
if [ -z "${GITHUB_PRIVATE_KEY_PATH:-}" ] || [ -z "${GITHUB_APP_ID:-}" ] || [ -z "${WEBHOOK_SECRET:-}" ]; then
    echo "ERROR: Required secrets not configured in ${ENV_FILE}"
    echo ""
    echo "Please set the following variables:"
    echo "  - GITHUB_PRIVATE_KEY_PATH"
    echo "  - GITHUB_APP_ID"
    echo "  - WEBHOOK_SECRET"
    echo ""
    echo "See the template file for instructions on obtaining these values."
    exit 1
fi

# Validate GitHub private key file exists
if [ ! -f "${GITHUB_PRIVATE_KEY_PATH}" ]; then
    echo "ERROR: GitHub private key file not found: ${GITHUB_PRIVATE_KEY_PATH}"
    echo ""
    echo "Please update GITHUB_PRIVATE_KEY_PATH in ${ENV_FILE} to point to your .pem file"
    exit 1
fi

# Get Konflux CR file path (default: my-konflux.yaml)
KONFLUX_CR="${1:-${REPO_ROOT}/my-konflux.yaml}"

if [ ! -f "${KONFLUX_CR}" ]; then
    echo "ERROR: Konflux CR file not found: ${KONFLUX_CR}"
    echo ""
    echo "Please create it from the template:"
    echo "  cp my-konflux.yaml.template my-konflux.yaml"
    echo ""
    echo "Then edit my-konflux.yaml with your configuration."
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
echo "  Demo users:  ${DEPLOY_DEMO_RESOURCES:-0}"
echo ""

# Step 1: Setup Kind cluster
echo "========================================="
echo "Step 1: Creating Kind cluster"
echo "========================================="
"${SCRIPT_DIR}/setup-kind-local-cluster.sh"

# Step 2: Build and load operator image
echo ""
echo "========================================="
echo "Step 2: Building operator image"
echo "========================================="
cd "${REPO_ROOT}/operator"
make docker-build IMG=konflux-operator:local

echo ""
echo "Loading operator image into Kind cluster..."
kind load docker-image konflux-operator:local --name konflux

# Step 3: Install CRDs and deploy operator
echo ""
echo "========================================="
echo "Step 3: Deploying Konflux operator"
echo "========================================="
echo "Installing CRDs..."
make install

echo ""
echo "Deploying operator..."
make deploy IMG=konflux-operator:local

cd "${REPO_ROOT}"

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
    kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
        --from-file=github-private-key="${GITHUB_PRIVATE_KEY_PATH}" \
        --from-literal=github-application-id="${GITHUB_APP_ID}" \
        --from-literal=webhook.secret="${WEBHOOK_SECRET}" \
        --dry-run=client -o yaml | kubectl apply -f -
done

echo "✓ Secrets created"

# Step 7: Optional demo resources
if [[ "${DEPLOY_DEMO_RESOURCES:-0}" -eq 1 ]]; then
    echo ""
    echo "========================================="
    echo "Step 7: Deploying demo resources"
    echo "========================================="
    "${SCRIPT_DIR}/deploy-demo-resources.sh"
else
    echo ""
    echo "========================================="
    echo "Step 7: Skipping demo resources"
    echo "========================================="
    echo "Demo resources deployment is disabled (secure default)"
    echo ""
    echo "To enable demo users, set DEPLOY_DEMO_RESOURCES=1 in ${ENV_FILE}"
    echo "Or deploy them separately: ./scripts/deploy-demo-resources.sh"
fi

# Step 8: Wait for Konflux to be ready
echo ""
echo "========================================="
echo "Step 8: Waiting for Konflux to be ready"
echo "========================================="
echo "This may take several minutes..."

if ! kubectl wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
    konflux/konflux \
    --timeout=15m 2>/dev/null; then
    echo ""
    echo "WARNING: Konflux CR did not become Ready within 15 minutes"
    echo "         This may be normal if deploying all components"
    echo "         Check status with: kubectl get konflux konflux -o yaml"
    echo ""
    echo "To monitor progress:"
    echo "  kubectl get pods -A"
    echo "  kubectl get konflux konflux -o jsonpath='{.status.conditions}' | jq"
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

if [[ "${DEPLOY_DEMO_RESOURCES:-0}" -eq 1 ]]; then
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

echo "Useful commands:"
echo "  kubectl get konflux konflux -o yaml    # View Konflux status"
echo "  kubectl get pods -A                     # View all pods"
echo "  kubectl logs -n konflux-operator deployment/konflux-operator-controller-manager"
echo ""
echo "To deploy demo resources later:"
echo "  ./scripts/deploy-demo-resources.sh"
echo ""
