#!/bin/bash -eu

# Setup Kind Local Cluster for Konflux
#
# This script sets up a local Kind (Kubernetes in Docker) cluster with proper
# configuration for Konflux development. It handles platform-specific setup
# (macOS/Linux), validates resources, and configures networking.
#
# This script is for LOCAL DEVELOPMENT CONVENIENCE ONLY. The Konflux operator
# and its components work on ANY Kubernetes cluster. This script just automates
# the common setup tasks for local Kind clusters.
#
# Prerequisites:
# - kind, podman/docker
#
# Configuration:
# Set these environment variables:
# - KIND_MEMORY_GB: Memory allocated to Kind (default: 8)
# - PODMAN_MACHINE_NAME: Specific Podman machine to use (macOS only, optional)
# - REGISTRY_HOST_PORT: Host port for registry (default: 5001)
# - ENABLE_REGISTRY_PORT: Enable registry port binding (default: 1)
# - INCREASE_PODMAN_PIDS_LIMIT: Increase PID limits (default: 1)

# Determine the absolute path of the repository root
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

# Set defaults for optional variables
KIND_MEMORY_GB="${KIND_MEMORY_GB:-8}"
PODMAN_MACHINE_NAME="${PODMAN_MACHINE_NAME:-}"
INCREASE_PODMAN_PIDS_LIMIT="${INCREASE_PODMAN_PIDS_LIMIT:-1}"
ENABLE_REGISTRY_PORT="${ENABLE_REGISTRY_PORT:-1}"
REGISTRY_HOST_PORT="${REGISTRY_HOST_PORT:-5001}"

# Increase inotify limits on Linux (only if current values are lower than required)
if [[ "$(uname)" == "Linux" ]]; then
    WATCHES_REQUIRED=524288
    INSTANCES_REQUIRED=512
    WATCHES_CURRENT=$(cat /proc/sys/fs/inotify/max_user_watches 2>/dev/null || echo 0)
    INSTANCES_CURRENT=$(cat /proc/sys/fs/inotify/max_user_instances 2>/dev/null || echo 0)
    if [[ "$WATCHES_CURRENT" -lt "$WATCHES_REQUIRED" ]] || [[ "$INSTANCES_CURRENT" -lt "$INSTANCES_REQUIRED" ]]; then
        echo "Increasing inotify limits for Kind cluster..."
        echo "  Current:  max_user_watches=${WATCHES_CURRENT}, max_user_instances=${INSTANCES_CURRENT}"
        echo "  Required: max_user_watches=${WATCHES_REQUIRED}, max_user_instances=${INSTANCES_REQUIRED}"
        echo ""
        echo "You may be prompted for your password. If you prefer, cancel and run"
        echo "these commands yourself, then rerun this script:"
        echo "  sudo sysctl fs.inotify.max_user_watches=${WATCHES_REQUIRED}"
        echo "  sudo sysctl fs.inotify.max_user_instances=${INSTANCES_REQUIRED}"
        echo ""
        sudo sysctl fs.inotify.max_user_watches="${WATCHES_REQUIRED}"
        sudo sysctl fs.inotify.max_user_instances="${INSTANCES_REQUIRED}"
    else
        echo "inotify limits already sufficient (watches=${WATCHES_CURRENT}, instances=${INSTANCES_CURRENT})."
    fi
fi

# Podman machine setup and validation on macOS
ORIGINAL_PODMAN_DEFAULT=""

# Function to restore original Podman default
restore_podman_default() {
    if [ -n "${ORIGINAL_PODMAN_DEFAULT}" ] && [ "${ORIGINAL_PODMAN_DEFAULT}" != "${PODMAN_MACHINE_NAME:-}" ]; then
        echo "Restoring original Podman default: ${ORIGINAL_PODMAN_DEFAULT}"
        podman system connection default "${ORIGINAL_PODMAN_DEFAULT}" 2>/dev/null || true
    fi
}

if [[ "$(uname)" == "Darwin" ]] && command -v podman &> /dev/null; then
    # If a specific Podman machine is requested, switch to it
    if [ -n "${PODMAN_MACHINE_NAME}" ]; then
        echo "Switching to Podman machine: ${PODMAN_MACHINE_NAME}"

        # Save the current default for restoration later
        ORIGINAL_PODMAN_DEFAULT=$(podman system connection list --format '{{.Name}} {{.Default}}' | grep true | awk '{print $1}')

        # Set trap to restore default on exit (success or failure)
        trap restore_podman_default EXIT

        # Ensure the machine exists
        if ! podman machine list --format "{{.Name}}" | sed 's/\*$//' | grep -q "^${PODMAN_MACHINE_NAME}$"; then
            echo "ERROR: Podman machine '${PODMAN_MACHINE_NAME}' does not exist."
            echo "Create it with:"
            echo "  podman machine init --memory $((KIND_MEMORY_GB * 1024 + 4096)) --cpus 6 --disk-size 100 --rootful ${PODMAN_MACHINE_NAME}"
            echo "  podman machine start ${PODMAN_MACHINE_NAME}"
            exit 1
        fi

        # Ensure the machine is running
        if ! podman machine list --format "{{.Name}} {{.Running}}" | grep "^${PODMAN_MACHINE_NAME}" | grep -q "true"; then
            echo "Starting Podman machine ${PODMAN_MACHINE_NAME}..."
            podman machine start "${PODMAN_MACHINE_NAME}"
        fi

        # Set as default connection
        podman system connection default "${PODMAN_MACHINE_NAME}"
        echo "Switched to Podman machine: ${PODMAN_MACHINE_NAME}"
    fi

    echo "Validating Podman machine configuration..."

    # Calculate required memory in MB (KIND_MEMORY_GB + 4GB overhead)
    KIND_MEMORY_MB=$((KIND_MEMORY_GB * 1024))
    REQUIRED_MEMORY_MB=$((KIND_MEMORY_MB + 4096))

    # Get current Podman machine memory (in MB)
    MACHINE_TO_INSPECT="${PODMAN_MACHINE_NAME:-}"
    if [ -z "${MACHINE_TO_INSPECT}" ]; then
        PODMAN_MEMORY=$(podman machine inspect 2>/dev/null | grep '"Memory"' | awk -F': ' '{print $2}' | tr -d ',')
    else
        PODMAN_MEMORY=$(podman machine inspect "${MACHINE_TO_INSPECT}" 2>/dev/null | grep '"Memory"' | awk -F': ' '{print $2}' | tr -d ',')
    fi

    if [ -n "${PODMAN_MEMORY}" ] && [ "${PODMAN_MEMORY}" -lt "${REQUIRED_MEMORY_MB}" ]; then
        echo "ERROR: Insufficient Podman machine memory."
        echo "  KIND_MEMORY_GB: ${KIND_MEMORY_GB}GB (${KIND_MEMORY_MB}MB)"
        echo "  Required Podman memory (with 4GB overhead): ${REQUIRED_MEMORY_MB}MB"
        echo "  Current Podman machine memory: ${PODMAN_MEMORY}MB"
        echo ""
        echo "To fix this, create a new Podman machine with more memory:"
        echo "  podman machine init --memory ${REQUIRED_MEMORY_MB} --cpus 6 --disk-size 100 --rootful <machine-name>"
        echo "  podman machine start <machine-name>"
        echo ""
        echo "Or reduce KIND_MEMORY_GB in scripts/deploy-local.env"
        exit 1
    fi

    echo "Podman machine has sufficient memory: ${PODMAN_MEMORY}MB >= ${REQUIRED_MEMORY_MB}MB"
fi

# Check for existing cluster
KIND_CLUSTER="${KIND_CLUSTER:-konflux}"
if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER}$"; then
    # Cluster exists - check if it's usable
    if kubectl --context "kind-${KIND_CLUSTER}" cluster-info &>/dev/null; then
        echo "Kind cluster '${KIND_CLUSTER}' already exists and is usable."
        echo "Skipping cluster creation. Delete it first if you want to recreate:"
        echo "  kind delete cluster --name ${KIND_CLUSTER}"
        exit 0
    else
        echo "Kind cluster '${KIND_CLUSTER}' exists but is not responding."
        echo "Deleting and recreating..."
        kind delete cluster --name "${KIND_CLUSTER}"
    fi
fi

# Check for port conflicts if registry port binding is enabled
if [[ "${ENABLE_REGISTRY_PORT}" -eq 1 ]]; then
    echo "Registry port binding is enabled. Checking if port ${REGISTRY_HOST_PORT} is available..."

    # Check if the port is in use
    if command -v lsof &> /dev/null; then
        if lsof -i ":${REGISTRY_HOST_PORT}" >/dev/null 2>&1; then
            echo "ERROR: Port ${REGISTRY_HOST_PORT} is already in use."
            echo ""
            echo "Port ${REGISTRY_HOST_PORT} is currently bound by another process:"
            lsof -i ":${REGISTRY_HOST_PORT}"
            echo ""
            echo "To resolve this issue, you have several options:"
            echo "  1. Stop the service using port ${REGISTRY_HOST_PORT}"
            echo "  2. Choose a different port by setting REGISTRY_HOST_PORT in scripts/deploy-local.env"
            echo "  3. Disable registry port binding by setting ENABLE_REGISTRY_PORT=0 in scripts/deploy-local.env"
            echo ""
            echo "Note: On macOS, port 5000 is often used by AirPlay Receiver."
            echo "      You can disable it in System Settings > General > AirDrop & Handoff > AirPlay Receiver"
            exit 1
        fi
    elif command -v ss &> /dev/null; then
        if ss -ltn "sport = :${REGISTRY_HOST_PORT}" | grep -q ":${REGISTRY_HOST_PORT}"; then
            echo "ERROR: Port ${REGISTRY_HOST_PORT} is already in use."
            echo "To resolve: stop the service, change REGISTRY_HOST_PORT, or set ENABLE_REGISTRY_PORT=0"
            exit 1
        fi
    elif command -v netstat &> /dev/null; then
        if netstat -an | grep -q "[:.]${REGISTRY_HOST_PORT}.*LISTEN"; then
            echo "ERROR: Port ${REGISTRY_HOST_PORT} is already in use."
            echo "To resolve: stop the service, change REGISTRY_HOST_PORT, or set ENABLE_REGISTRY_PORT=0"
            exit 1
        fi
    else
        echo "WARNING: Unable to check port availability (lsof, ss, and netstat not found)."
        echo "         Proceeding anyway, but cluster creation may fail if port ${REGISTRY_HOST_PORT} is in use."
    fi

    echo "Port ${REGISTRY_HOST_PORT} is available."
fi

# Use unified kind config (supports all architectures)
KIND_CONFIG="${REPO_ROOT}/kind-config.yaml"

# Update kind config with configured memory
echo "Configuring Kind cluster with ${KIND_MEMORY_GB}Gi memory..."
sed -i.bak "s/system-reserved: memory=.*/system-reserved: memory=${KIND_MEMORY_GB}Gi/" "${KIND_CONFIG}" && rm "${KIND_CONFIG}.bak"

# Configure registry port mapping
if [[ "${ENABLE_REGISTRY_PORT}" -eq 1 ]]; then
    if [[ "${REGISTRY_HOST_PORT}" != "5001" ]]; then
        echo "Configuring registry port mapping to host port ${REGISTRY_HOST_PORT}..."
        sed -i.bak "s/hostPort: 5001/hostPort: ${REGISTRY_HOST_PORT}/" "${KIND_CONFIG}" && rm "${KIND_CONFIG}.bak"
    else
        echo "Using default registry port mapping (host port 5001)..."
    fi
else
    echo "Registry port binding is disabled. Removing registry port mapping..."
    sed -i.bak '/# Registry/,+3d' "${KIND_CONFIG}" && rm "${KIND_CONFIG}.bak"
fi

# Create the Kind cluster
KIND_CLUSTER="${KIND_CLUSTER:-konflux}"
echo "Creating Kind cluster '${KIND_CLUSTER}'..."
kind create cluster --name "${KIND_CLUSTER}" --config "${KIND_CONFIG}"

# Revert kind config changes
echo "Reverting kind config to original state..."
(cd "${REPO_ROOT}" && git checkout kind-config.yaml 2>/dev/null || true)

sleep 2

# Optionally increase the Podman PID limit if the feature is enabled and Podman is the active runtime.
if [[ "${INCREASE_PODMAN_PIDS_LIMIT}" -eq 1 ]] && \
   command -v podman &> /dev/null && \
   [ -n "$(podman ps -q --filter 'name=^konflux-control-plane$')" ]; then
    echo "Increasing Podman PID limit for better Tekton performance..."
    podman update --pids-limit 8192 konflux-control-plane
else
    echo "Skipping Podman PID limit increase."
fi

echo "✓ Kind cluster 'konflux' created successfully"
echo ""
echo "Next steps:"
echo "  1. Deploy dependencies: ./scripts/deploy-deps.sh"
echo "  2. Deploy operator: cd operator && make deploy"
echo "  3. Apply Konflux CR: kubectl apply -f my-konflux.yaml"
echo ""
echo "Or use the all-in-one script: ./scripts/deploy-local.sh"
