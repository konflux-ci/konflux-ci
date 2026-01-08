#!/bin/bash -eu

# End-to-End (e2e) Konflux Test Environment Setup Script
#
# This script automates the setup of a local Konflux development and testing
# environment using Kind (Kubernetes in Docker). It deploys all the necessary
# dependencies, Konflux services, and test resources.
#
# --- Prerequisites ---
#
# Please ensure you have the following command-line tools installed:
# - kind: brew install kind
# - podman: brew install podman
# - kubectl: brew install kubectl
# - A password manager CLI for secret handling is recommended (e.g., pass, 1password-cli).
#
# --- Configuration ---
#
# Before running this script, you must configure your environment by setting up
# two files. If these files are not configured, the script will exit with an error.
#
# 1. Smee Channel ID for GitHub Webhooks:
#    - Go to https://smee.io and create a new channel.
#    - Copy the template file:
#      cp scripts/smee-channel-id.tpl scripts/smee-channel-id.yaml
#    - Edit scripts/smee-channel-id.yaml and set the 'url' field to your Smee channel URL.
#
# 2. Environment Variables (Secrets & IDs):
#    - This file handles your Quay token, GitHub App details, and other secrets.
#    - Copy the template file:
#      cp scripts/deploy-e2e.env.template scripts/deploy-e2e.env
#    - Edit scripts/deploy-e2e.env and fill in the required values. See the comments in the
#      template file for detailed instructions on creating a Quay Robot Account
#      and a GitHub App.
#
# After completing the configuration steps, you can run the script from the repository root:
# ./scripts/deploy-e2e.sh


# Determine the absolute path of the repository root
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

# Check for smee configuration file in scripts directory first, then fall back to dependencies directory
SMEE_CONFIG_SCRIPTS="${SCRIPT_DIR}/smee-channel-id.yaml"
SMEE_CONFIG_DEPS="${REPO_ROOT}/dependencies/smee/smee-channel-id.yaml"

if [ -f "${SMEE_CONFIG_SCRIPTS}" ]; then
	echo "Found smee configuration in scripts directory, copying to dependencies/smee/..."
	cp "${SMEE_CONFIG_SCRIPTS}" "${SMEE_CONFIG_DEPS}"
elif [ ! -f "${SMEE_CONFIG_DEPS}" ]; then
	echo "Error: Smee configuration not found."
	echo "Please copy scripts/smee-channel-id.tpl to scripts/smee-channel-id.yaml and set the URL values."
	echo "Alternatively, you can create dependencies/smee/smee-channel-id.yaml directly."
	exit 1
fi

ENV_FILE="${SCRIPT_DIR}/deploy-e2e.env"
if [ ! -f "${ENV_FILE}" ]; then
    echo "Configuration file ${ENV_FILE} not found."
    echo "Please copy scripts/deploy-e2e.env.template to ${ENV_FILE} and fill in the required values."
    exit 1
fi
# shellcheck disable=SC1090
source "${ENV_FILE}"

# Set defaults for optional variables
KIND_MEMORY_GB="${KIND_MEMORY_GB:-8}"
PODMAN_MACHINE_NAME="${PODMAN_MACHINE_NAME:-}"
INCREASE_PODMAN_PIDS_LIMIT="${INCREASE_PODMAN_PIDS_LIMIT:-1}"
QUAY_TOKEN="${QUAY_TOKEN:-}"
ENABLE_REGISTRY_PORT="${ENABLE_REGISTRY_PORT:-1}"
REGISTRY_HOST_PORT="${REGISTRY_HOST_PORT:-5001}"

# Validate that the required variables are set
if [ -z "${GITHUB_PRIVATE_KEY_PATH}" ] || [ -z "${GITHUB_APP_ID}" ] || [ -z "${WEBHOOK_SECRET}" ]; then
    echo "Error: GITHUB_PRIVATE_KEY_PATH, GITHUB_APP_ID, and WEBHOOK_SECRET must be set in scripts/deploy-e2e.env"
    exit 1
fi

if [[ "$(uname)" == "Linux" ]]; then
    echo "This script needs to run 'sudo sysctl' to increase inotify limits for the Kind cluster."
    echo "You may be prompted for your password."
	sudo sysctl fs.inotify.max_user_watches=524288
	sudo sysctl fs.inotify.max_user_instances=512
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

        # Ensure the machine exists (strip asterisk for default machine)
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

    # Calculate required memory in MB (KIND_MEMORY_GB + 3GB overhead)
    KIND_MEMORY_MB=$((KIND_MEMORY_GB * 1024))
    REQUIRED_MEMORY_MB=$((KIND_MEMORY_MB + 3072))

    # Get current Podman machine memory (in MB)
    # If a specific machine is configured, inspect that one; otherwise inspect the default
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
        echo "Or reduce KIND_MEMORY_GB in scripts/deploy-e2e.env"
        exit 1
    fi

    echo "Podman machine has sufficient memory: ${PODMAN_MEMORY}MB >= ${REQUIRED_MEMORY_MB}MB"
fi

kind delete cluster --name konflux || echo ok.

# Check for port conflicts if registry port binding is enabled
# This check happens AFTER cluster deletion to ensure any previous deployment ports are freed
if [[ "${ENABLE_REGISTRY_PORT}" -eq 1 ]]; then
    echo "Registry port binding is enabled. Checking if port ${REGISTRY_HOST_PORT} is available..."

    # Check if the port is in use (works on both Linux and macOS)
    if command -v lsof &> /dev/null; then
        # Use lsof if available (macOS and most Linux)
        if lsof -i ":${REGISTRY_HOST_PORT}" >/dev/null 2>&1; then
            echo "ERROR: Port ${REGISTRY_HOST_PORT} is already in use."
            echo ""
            echo "Port ${REGISTRY_HOST_PORT} is currently bound by another process:"
            lsof -i ":${REGISTRY_HOST_PORT}"
            echo ""
            echo "To resolve this issue, you have several options:"
            echo "  1. Stop the service using port ${REGISTRY_HOST_PORT}"
            echo "  2. Choose a different port by setting REGISTRY_HOST_PORT in scripts/deploy-e2e.env"
            echo "  3. Disable registry port binding by setting ENABLE_REGISTRY_PORT=0 in scripts/deploy-e2e.env"
            echo ""
            echo "Note: On macOS, port 5000 is often used by AirPlay Receiver (ControlCenter)."
            echo "      You can disable it in System Settings > General > AirDrop & Handoff > AirPlay Receiver"
            exit 1
        fi
    elif command -v ss &> /dev/null; then
        # Use ss on Linux systems without lsof
        if ss -ltn "sport = :${REGISTRY_HOST_PORT}" | grep -q ":${REGISTRY_HOST_PORT}"; then
            echo "ERROR: Port ${REGISTRY_HOST_PORT} is already in use."
            echo ""
            echo "To resolve this issue, you have several options:"
            echo "  1. Stop the service using port ${REGISTRY_HOST_PORT}"
            echo "  2. Choose a different port by setting REGISTRY_HOST_PORT in scripts/deploy-e2e.env"
            echo "  3. Disable registry port binding by setting ENABLE_REGISTRY_PORT=0 in scripts/deploy-e2e.env"
            exit 1
        fi
    elif command -v netstat &> /dev/null; then
        # Fallback to netstat (widely available but deprecated on many systems)
        if netstat -an | grep -q "[:.]${REGISTRY_HOST_PORT}.*LISTEN"; then
            echo "ERROR: Port ${REGISTRY_HOST_PORT} is already in use."
            echo ""
            echo "To resolve this issue, you have several options:"
            echo "  1. Stop the service using port ${REGISTRY_HOST_PORT}"
            echo "  2. Choose a different port by setting REGISTRY_HOST_PORT in scripts/deploy-e2e.env"
            echo "  3. Disable registry port binding by setting ENABLE_REGISTRY_PORT=0 in scripts/deploy-e2e.env"
            exit 1
        fi
    else
        echo "WARNING: Unable to check port availability (lsof, ss, and netstat not found)."
        echo "         Proceeding anyway, but cluster creation may fail if port ${REGISTRY_HOST_PORT} is in use."
    fi

    echo "Port ${REGISTRY_HOST_PORT} is available."
fi

# Select appropriate kind config based on architecture
HOST_ARCH=$(uname -m)
if [[ "$HOST_ARCH" == "arm64" ]] || [[ "$HOST_ARCH" == "aarch64" ]]; then
    echo "Detected ARM64 architecture - using kind-config-arm64.yaml with QEMU support..."
    KIND_CONFIG="${REPO_ROOT}/kind-config-arm64.yaml"
else
    KIND_CONFIG="${REPO_ROOT}/kind-config.yaml"
fi

# Update kind config with configured memory
echo "Configuring Kind cluster with ${KIND_MEMORY_GB}Gi memory..."
sed -i.bak "s/system-reserved: memory=.*/system-reserved: memory=${KIND_MEMORY_GB}Gi/" "${KIND_CONFIG}" && rm "${KIND_CONFIG}.bak"

# Configure registry port mapping
if [[ "${ENABLE_REGISTRY_PORT}" -eq 1 ]]; then
    # Update the registry port if different from default 5001
    if [[ "${REGISTRY_HOST_PORT}" != "5001" ]]; then
        echo "Configuring registry port mapping to host port ${REGISTRY_HOST_PORT}..."
        sed -i.bak "s/hostPort: 5001/hostPort: ${REGISTRY_HOST_PORT}/" "${KIND_CONFIG}" && rm "${KIND_CONFIG}.bak"
    else
        echo "Using default registry port mapping (host port 5001)..."
    fi
else
    echo "Registry port binding is disabled. Removing registry port mapping from kind config..."
    # Remove the registry port mapping section (lines from "# Registry" comment through the TCP protocol line)
    sed -i.bak '/# Registry/,+3d' "${KIND_CONFIG}" && rm "${KIND_CONFIG}.bak"
fi

kind create cluster --name konflux --config "${KIND_CONFIG}"

# Revert kind config changes
echo "Reverting kind config to original state..."
(cd "${REPO_ROOT}" && git checkout kind-config.yaml kind-config-arm64.yaml 2>/dev/null || true)

sleep 2

# Optionally increase the Podman PID limit if the feature is enabled and Podman is the active runtime.
if [[ "${INCREASE_PODMAN_PIDS_LIMIT}" -eq 1 ]] && \
   command -v podman &> /dev/null && \
   [ -n "$(podman ps -q --filter 'name=^konflux-control-plane$')" ]; then
    echo "Podman runtime detected and INCREASE_PODMAN_PIDS_LIMIT is 1. Increasing PID limit..."
    podman update --pids-limit 8192 konflux-control-plane
else
    echo "Skipping Podman PID limit increase."
fi

"${SCRIPT_DIR}/deploy-deps.sh"
"${SCRIPT_DIR}/deploy-konflux.sh"
if [ -n "${QUAY_TOKEN:-}" ]; then
    echo "QUAY_TOKEN is set. Deploying image-controller..."
    "${SCRIPT_DIR}/deploy-image-controller.sh" "${QUAY_TOKEN}" experiment
else
    echo "QUAY_TOKEN is not set. Skipping image-controller deployment."
fi
"${SCRIPT_DIR}/deploy-test-resources.sh"

echo "Creating PaC secrets"
for ns in pipelines-as-code build-service integration-service; do
	echo "Creating PaC secret for $ns"
	kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
		--from-file=github-private-key="${GITHUB_PRIVATE_KEY_PATH}" \
		--from-literal=github-application-id="${GITHUB_APP_ID}" \
		--from-literal=webhook.secret="${WEBHOOK_SECRET}"
done

# Podman default restoration is handled by the trap on EXIT