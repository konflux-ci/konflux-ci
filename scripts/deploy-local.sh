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
#  - kustomize (only for 'checkout' / 'build' install methods)
#  - Configuration file: scripts/deploy-local.env
#
# Usage:
#   ./scripts/deploy-local.sh [konflux-cr-file]
#
# By default, uses operator/config/samples/konflux_v1alpha1_konflux.yaml
# (or the matching sample from a GitHub release when using OPERATOR_INSTALL_METHOD=release).
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
#   checkout (default) - Install from current checkout; image tagged with git SHA on Quay
#   release            - Install released install.yaml + released sample CR (same release)
#   build              - Build operator image locally and install (for operator developers)
#   none               - Skip operator install and Konflux CR (for running operator locally)
#
# The legacy value 'local' is accepted as an alias for 'checkout'.
#
# checkout image selection:
#   1. OPERATOR_IMAGE if set
#   2. else quay.io/konflux-ci/konflux-operator:<OPERATOR_GIT_SHA|HEAD>
# The image must exist on the registry (checked via docker/podman manifest inspect).
# If it does not, the script exits and suggests OPERATOR_INSTALL_METHOD=build.
#
# release selection (OPERATOR_RELEASE, default: latest):
#   OPERATOR_INSTALL_METHOD=release ./scripts/deploy-local.sh
#   OPERATOR_INSTALL_METHOD=release OPERATOR_RELEASE=v0.1.13 ./scripts/deploy-local.sh
#
# For 'none' method, the script sets up Kind + dependencies + secrets, then exits.
# You then run the operator yourself:
#   cd operator && make install && make run

set -euo pipefail

# Determine the absolute path of the repository root
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

OPERATOR_IMAGE_REPO="${OPERATOR_IMAGE_REPO:-quay.io/konflux-ci/konflux-operator}"
RELEASE_DOWNLOAD_BASE="${RELEASE_DOWNLOAD_BASE:-https://github.com/konflux-ci/konflux-ci/releases}"

# Prefer docker then podman — same selection order as operator/Makefile CONTAINER_TOOL.
resolve_container_tool() {
    if [ -n "${CONTAINER_TOOL:-}" ]; then
        echo "${CONTAINER_TOOL}"
        return
    fi
    if command -v docker >/dev/null 2>&1; then
        echo docker
        return
    fi
    if command -v podman >/dev/null 2>&1; then
        echo podman
        return
    fi
    echo ""
}

# Return 0 if the image ref exists in a remote registry.
remote_image_exists() {
    local image="$1"
    local tool="$2"
    # Both docker and podman support `manifest inspect` for remote refs.
    "${tool}" manifest inspect "${image}" >/dev/null 2>&1
}

suggest_checkout_alternatives() {
    local image="$1"
    local sha="$2"
    cat >&2 <<EOF

No image found at: ${image}

This usually means the commit has not been built yet (local/unpushed work, or the
push build is still running).

Options:
  1. Build and deploy locally (typical for unbuilt local commits):
       OPERATOR_INSTALL_METHOD=build ./scripts/deploy-local.sh

  2. Use a SHA that already has an image on Quay:
       OPERATOR_GIT_SHA=<sha-with-image> OPERATOR_INSTALL_METHOD=checkout ./scripts/deploy-local.sh
     or:
       git checkout <sha-with-image>

  3. Use a GitHub release (released install.yaml + released sample CR):
       OPERATOR_INSTALL_METHOD=release ./scripts/deploy-local.sh
     or pin a specific release tag:
       OPERATOR_INSTALL_METHOD=release OPERATOR_RELEASE=v0.1.13 ./scripts/deploy-local.sh

Current git SHA considered: ${sha}
EOF
}

release_asset_url() {
    local release="$1"
    local asset="$2"
    if [ "${release}" = "latest" ]; then
        echo "${RELEASE_DOWNLOAD_BASE}/latest/download/${asset}"
    else
        echo "${RELEASE_DOWNLOAD_BASE}/download/${release}/${asset}"
    fi
}

# Download and extract release samples into a repo-local directory; print the path.
# Expected layout matches generate-release-artifacts.sh: flat YAML at the archive root
# (e.g. konflux_v1alpha1_konflux.yaml), not nested under a subdirectory.
# Directory is gitignored (.tmp/); cleared at the start of each fetch.
fetch_release_samples() {
    local release="$1"
    local url samples_dir
    url="$(release_asset_url "${release}" "samples.tar.gz")"
    samples_dir="${REPO_ROOT}/.tmp/release-samples"
    rm -rf "${samples_dir}"
    mkdir -p "${samples_dir}"
    echo "Downloading release samples: ${url}" >&2
    if ! curl -fsSL "${url}" | tar -xzf - -C "${samples_dir}"; then
        echo "ERROR: Failed to download or extract release samples from ${url}" >&2
        rm -rf "${samples_dir}"
        exit 1
    fi
    if [ ! -f "${samples_dir}/konflux_v1alpha1_konflux.yaml" ]; then
        echo "ERROR: Release samples missing expected file konflux_v1alpha1_konflux.yaml in ${samples_dir}" >&2
        echo "Expected flat YAML at archive root (as produced by generate-release-artifacts.sh)." >&2
        ls -la "${samples_dir}" >&2
        rm -rf "${samples_dir}"
        exit 1
    fi
    echo "${samples_dir}"
}

verify_release_assets() {
    local release="$1"
    local install_url samples_url
    install_url="$(release_asset_url "${release}" "install.yaml")"
    samples_url="$(release_asset_url "${release}" "samples.tar.gz")"
    echo "Verifying release assets for '${release}'..."
    if ! curl -fsSIL "${install_url}" >/dev/null; then
        echo "ERROR: install.yaml not reachable: ${install_url}" >&2
        exit 1
    fi
    if ! curl -fsSIL "${samples_url}" >/dev/null; then
        echo "ERROR: samples.tar.gz not reachable: ${samples_url}" >&2
        exit 1
    fi
    echo "✓ Release assets reachable"
    echo "  install: ${install_url}"
    echo "  samples: ${samples_url}"
}

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
OPERATOR_INSTALL_METHOD="${OPERATOR_INSTALL_METHOD:-checkout}"
OPERATOR_RELEASE="${OPERATOR_RELEASE:-latest}"
SKIP_SECRETS="${SKIP_SECRETS:-false}"

# Export variables for child scripts
export KIND_CLUSTER KIND_MEMORY_GB PODMAN_MACHINE_NAME REGISTRY_HOST_PORT ENABLE_REGISTRY_PORT
export INCREASE_PODMAN_PIDS_LIMIT ENABLE_IMAGE_CACHE
export GITHUB_PRIVATE_KEY GITHUB_PRIVATE_KEY_PATH GITHUB_APP_ID WEBHOOK_SECRET QUAY_TOKEN QUAY_ORGANIZATION QUAY_API_URL

# Child scripts only see exported variables (values from deploy-local.env are not
# exported by sourcing); validate secrets after exports so checks see the same env.
[ "${SKIP_SECRETS}" = "true" ] || VALIDATE_ONLY=true "${SCRIPT_DIR}/deploy-secrets.sh"

INSTALL_METHOD="${OPERATOR_INSTALL_METHOD}"
if [ "${INSTALL_METHOD}" = "local" ]; then
    echo "WARNING: OPERATOR_INSTALL_METHOD=local is deprecated; use 'checkout' instead."
    echo "         Continuing with checkout (checkout manifests + SHA-tagged Quay image)."
    INSTALL_METHOD="checkout"
fi

case "${INSTALL_METHOD}" in
    checkout|release|build|none) ;;
    *)
        echo "ERROR: Invalid OPERATOR_INSTALL_METHOD: ${INSTALL_METHOD}" >&2
        echo "Valid options: checkout, release, build, none (legacy alias: local)" >&2
        exit 1
        ;;
esac

# Resolve operator image (and git SHA only for checkout — release/none/build
# must not require a .git directory).
OPERATOR_IMG=""
RELEASE_SAMPLES_DIR=""
CONTAINER_TOOL="$(resolve_container_tool)"

case "${INSTALL_METHOD}" in
    checkout)
        if [ -z "${CONTAINER_TOOL}" ]; then
            echo "ERROR: Neither docker nor podman is available." >&2
            echo "Install one of them (same requirement as operator/Makefile CONTAINER_TOOL)." >&2
            exit 1
        fi
        if [ -n "${OPERATOR_IMAGE:-}" ]; then
            OPERATOR_IMG="${OPERATOR_IMAGE}"
            # SHA is optional when an explicit image is provided (for error messaging only).
            OPERATOR_GIT_SHA="${OPERATOR_GIT_SHA:-}"
        else
            OPERATOR_GIT_SHA="${OPERATOR_GIT_SHA:-$(git -C "${REPO_ROOT}" rev-parse HEAD)}"
            OPERATOR_IMG="${OPERATOR_IMAGE_REPO}:${OPERATOR_GIT_SHA}"
        fi
        echo "Using container tool: ${CONTAINER_TOOL}"
        echo "Checking operator image exists: ${OPERATOR_IMG}"
        if ! remote_image_exists "${OPERATOR_IMG}" "${CONTAINER_TOOL}"; then
            echo "ERROR: Operator image not found for checkout install method." >&2
            suggest_checkout_alternatives "${OPERATOR_IMG}" "${OPERATOR_GIT_SHA:-unknown}"
            exit 1
        fi
        echo "✓ Operator image found"
        ;;
    release)
        verify_release_assets "${OPERATOR_RELEASE}"
        ;;
    build)
        if [ -z "${CONTAINER_TOOL}" ]; then
            echo "ERROR: Neither docker nor podman is available (required to build the operator image)." >&2
            exit 1
        fi
        echo "Using container tool: ${CONTAINER_TOOL}"
        OPERATOR_IMG="${OPERATOR_IMAGE:-localhost/konflux-operator:local}"
        ;;
    none)
        ;;
esac

# Resolve Konflux CR
# For release: use samples from the same release unless the caller set KONFLUX_CR /
# passed a positional CR path (explicit override).
KONFLUX_CR_EXPLICIT="${1:-${KONFLUX_CR:-}}"
if [ "${INSTALL_METHOD}" = "release" ] && [ -z "${KONFLUX_CR_EXPLICIT}" ]; then
    RELEASE_SAMPLES_DIR="$(fetch_release_samples "${OPERATOR_RELEASE}")"
    export SAMPLES_DIR="${RELEASE_SAMPLES_DIR}"
    unset KONFLUX_CR
    KONFLUX_CR="$(SAMPLES_DIR="${RELEASE_SAMPLES_DIR}" "${SCRIPT_DIR}/resolve-konflux-cr.sh")"
else
    KONFLUX_CR="${KONFLUX_CR_EXPLICIT}"
    export KONFLUX_CR
    KONFLUX_CR=$("${SCRIPT_DIR}/resolve-konflux-cr.sh")
fi
export KONFLUX_CR

echo "========================================="
echo "Konflux Local Development Deployment"
echo "========================================="
echo ""
echo "Configuration:"
echo "  Environment:     ${ENV_FILE}"
echo "  Install method:  ${INSTALL_METHOD}"
echo "  Konflux CR:      ${KONFLUX_CR}"
if [ -n "${OPERATOR_IMG}" ]; then
    echo "  Operator image:  ${OPERATOR_IMG}"
fi
if [ "${INSTALL_METHOD}" = "checkout" ] && [ -n "${OPERATOR_GIT_SHA:-}" ]; then
    echo "  Git SHA:         ${OPERATOR_GIT_SHA}"
fi
if [ "${INSTALL_METHOD}" = "release" ]; then
    echo "  Release:         ${OPERATOR_RELEASE}"
fi
echo ""

# For 'build' method, build the operator image before creating the cluster to reduce peak memory (no Kind container during go build)
if [ "${INSTALL_METHOD}" = "build" ]; then
    echo "========================================="
    echo "Building operator image (before cluster)"
    echo "========================================="
    cd "${REPO_ROOT}/operator"
    # Ensure Makefile uses the same container tool selection when possible.
    make docker-build IMG="${OPERATOR_IMG}" CONTAINER_TOOL="${CONTAINER_TOOL}"
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
SET_SKIP_CHECKS="${SET_SKIP_CHECKS:-false}" \
"${REPO_ROOT}/deploy-deps.sh"

# Step 3: Deploy Konflux operator
echo ""
echo "========================================="
echo "Step 3: Deploying Konflux operator"
echo "========================================="
echo "Using installation method: ${INSTALL_METHOD}"

case "${INSTALL_METHOD}" in
    checkout)
        echo "Installing from current checkout with Quay image ${OPERATOR_IMG}..."
        cd "${REPO_ROOT}/operator"
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
        RELEASE_URL="$(release_asset_url "${OPERATOR_RELEASE}" "install.yaml")"
        echo "Installing from GitHub release (${OPERATOR_RELEASE})..."
        echo "Downloading: ${RELEASE_URL}"
        kubectl apply -f "${RELEASE_URL}"
        ;;

    none)
        echo "Skipping operator installation (OPERATOR_INSTALL_METHOD=none)"
        echo "You will need to run the operator manually after deployment completes:"
        echo "  cd operator && make install && make run"
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
if [ "${SKIP_SECRETS}" != "true" ]; then
  if [ "${INSTALL_METHOD}" = "none" ]; then
      env CREATE_NAMESPACES=true WAIT_FOR_PODS=false "${SCRIPT_DIR}/deploy-secrets.sh"
  else
      "${SCRIPT_DIR}/deploy-secrets.sh"
  fi

  echo "✓ Secrets created"
fi

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
