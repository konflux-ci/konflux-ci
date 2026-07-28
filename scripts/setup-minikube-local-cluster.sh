#!/usr/bin/env bash
set -euo pipefail

# Setup Minikube Local Cluster for Konflux
#
# This script sets up a local Minikube cluster with the Docker driver and proper
# configuration for Konflux development. It handles resource validation and
# configures networking (port mappings via NodePorts).
#
# This script is for LOCAL DEVELOPMENT CONVENIENCE ONLY. The Konflux operator
# and its components work on ANY Kubernetes cluster. This script just automates
# the common setup tasks for local Minikube clusters.
#
# Prerequisites:
# - minikube, docker
#
# Configuration:
# Set these environment variables:
# - MINIKUBE_MEMORY_MB: Memory allocated to Minikube (default: 8192)
# - MINIKUBE_CPUS: CPUs allocated to Minikube (default: 4)
# - MINIKUBE_DISK_SIZE: Disk size for Minikube (default: 100g)
# - MINIKUBE_PROFILE: Minikube profile name (default: konflux)
# - REGISTRY_HOST_PORT: Host port for registry (default: 5001)
# - ENABLE_REGISTRY_PORT: Enable registry port binding (default: 1)

# Determine the absolute path of this script's directory
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)

# Set defaults for optional variables
MINIKUBE_MEMORY_MB="${MINIKUBE_MEMORY_MB:-max}"
MINIKUBE_CPUS="${MINIKUBE_CPUS:-max}"
MINIKUBE_DISK_SIZE="${MINIKUBE_DISK_SIZE:-100g}"
MINIKUBE_PROFILE="${MINIKUBE_PROFILE:-konflux}"
ENABLE_REGISTRY_PORT="${ENABLE_REGISTRY_PORT:-1}"
REGISTRY_HOST_PORT="${REGISTRY_HOST_PORT:-5001}"

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------

if ! command -v minikube &> /dev/null; then
    echo "ERROR: minikube is not installed."
    echo "Install it from: https://minikube.sigs.k8s.io/docs/start/"
    exit 1
fi

if ! command -v docker &> /dev/null; then
    echo "ERROR: docker is not installed."
    echo "Install Docker Desktop or Docker Engine for your platform."
    exit 1
fi

# Verify Docker daemon is running
if ! docker info &> /dev/null; then
    echo "ERROR: Docker daemon is not running."
    echo "Start Docker and try again."
    exit 1
fi

# ---------------------------------------------------------------------------
# Increase inotify limits on Linux (only if current values are lower than required)
# ---------------------------------------------------------------------------

if [[ "$(uname)" == "Linux" ]]; then
    WATCHES_REQUIRED=524288
    INSTANCES_REQUIRED=512
    WATCHES_CURRENT=$(cat /proc/sys/fs/inotify/max_user_watches 2>/dev/null || echo 0)
    INSTANCES_CURRENT=$(cat /proc/sys/fs/inotify/max_user_instances 2>/dev/null || echo 0)
    if [[ "$WATCHES_CURRENT" -lt "$WATCHES_REQUIRED" ]] || [[ "$INSTANCES_CURRENT" -lt "$INSTANCES_REQUIRED" ]]; then
        echo "Increasing inotify limits for Minikube cluster..."
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

# ---------------------------------------------------------------------------
# Check for existing cluster
# ---------------------------------------------------------------------------

if minikube status -p "${MINIKUBE_PROFILE}" &>/dev/null; then
    if kubectl --context "${MINIKUBE_PROFILE}" cluster-info &>/dev/null; then
        echo "Minikube cluster '${MINIKUBE_PROFILE}' already exists and is usable."
        echo "Skipping cluster creation. Delete it first if you want to recreate:"
        echo "  minikube delete -p ${MINIKUBE_PROFILE}"
        exit 0
    else
        echo "Minikube cluster '${MINIKUBE_PROFILE}' exists but is not responding."
        echo "Deleting and recreating..."
        minikube delete -p "${MINIKUBE_PROFILE}"
    fi
fi

# ---------------------------------------------------------------------------
# Check for port conflicts if registry port binding is enabled
# ---------------------------------------------------------------------------

if [[ "${ENABLE_REGISTRY_PORT}" -eq 1 ]]; then
    echo "Registry port binding is enabled. Checking if port ${REGISTRY_HOST_PORT} is available..."

    if command -v lsof &> /dev/null; then
        if lsof -i ":${REGISTRY_HOST_PORT}" >/dev/null 2>&1; then
            echo "ERROR: Port ${REGISTRY_HOST_PORT} is already in use."
            echo ""
            echo "Port ${REGISTRY_HOST_PORT} is currently bound by another process:"
            lsof -i ":${REGISTRY_HOST_PORT}"
            echo ""
            echo "To resolve this issue, you have several options:"
            echo "  1. Stop the service using port ${REGISTRY_HOST_PORT}"
            echo "  2. Choose a different port by setting REGISTRY_HOST_PORT"
            echo "  3. Disable registry port binding by setting ENABLE_REGISTRY_PORT=0"
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
    else
        echo "WARNING: Unable to check port availability (lsof and ss not found)."
        echo "         Proceeding anyway, but cluster creation may fail if port ${REGISTRY_HOST_PORT} is in use."
    fi

    echo "Port ${REGISTRY_HOST_PORT} is available."
fi

# ---------------------------------------------------------------------------
# Build port mapping flags
# ---------------------------------------------------------------------------
# Minikube with Docker driver supports --ports for host:container port mappings.
# These mirror the Kind extraPortMappings from kind-config.yaml.

PORTS_FLAGS=(
    "--ports=8888:30010"    # Generic
    "--ports=9443:30011"    # UI
    "--ports=8180:30012"    # PaC
    "--ports=8443:30002"    # Quay
)

if [[ "${ENABLE_REGISTRY_PORT}" -eq 1 ]]; then
    PORTS_FLAGS+=("--ports=${REGISTRY_HOST_PORT}:30001")  # Registry
fi

# ---------------------------------------------------------------------------
# Create the Minikube cluster
# ---------------------------------------------------------------------------

echo "Creating Minikube cluster '${MINIKUBE_PROFILE}' with Docker driver..."
echo "  Memory: ${MINIKUBE_MEMORY_MB}MB"
echo "  CPUs:   ${MINIKUBE_CPUS}"
echo "  Disk:   ${MINIKUBE_DISK_SIZE}"

minikube start \
    -p "${MINIKUBE_PROFILE}" \
    --driver=docker \
    --memory="${MINIKUBE_MEMORY_MB}" \
    --cpus="${MINIKUBE_CPUS}" \
    --disk-size="${MINIKUBE_DISK_SIZE}" \
    --container-runtime=containerd \
    --extra-config=apiserver.service-account-issuer=https://kubernetes.default.svc \
    --extra-config=kubelet.serialize-image-pulls=false \
    "${PORTS_FLAGS[@]}"

# Label the node for ingress-ready (matches Kind config)
kubectl label node "${MINIKUBE_PROFILE}" ingress-ready=true --overwrite

sleep 2

echo ""
echo "Minikube cluster '${MINIKUBE_PROFILE}' created successfully"
echo ""
echo "Next steps:"
echo "  1. Deploy dependencies: ./deploy-deps.sh"
echo "  2. Deploy operator: cd operator && make deploy"
echo "  3. Apply Konflux CR: kubectl apply -f my-konflux.yaml"
echo ""
echo "Or use the all-in-one script: DEPLOY_DRIVER=minikube ./scripts/deploy-local.sh"
