#!/bin/bash
#
# Build all kustomizations in upstream-kustomizations directory
# and place the results in pkg/manifests directory
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_DIR="${SCRIPT_DIR}/../../upstream-kustomizations"
OUTPUT_DIR="${SCRIPT_DIR}"

# List of components to build
COMPONENTS=(
    "application-api"
    "build-service"
    "enterprise-contract"
    "image-controller"
    "integration"
    "namespace-lister"
    "rbac"
    "release"
    "ui"
)

# Iterate through each component
for dir_name in "${COMPONENTS[@]}"; do
    source_subdir="${SOURCE_DIR}/${dir_name}"
    output_subdir="${OUTPUT_DIR}/${dir_name}"

    if [[ ! -d "${source_subdir}" ]]; then
        echo "  ✗ Source directory ${source_subdir} does not exist" >&2
        exit 1
    fi

    echo "Building ${dir_name}..."
    mkdir -p "${output_subdir}"

    if kustomize build "${source_subdir}" > "${output_subdir}/manifests.yaml"; then
        echo "  ✓ Successfully built ${dir_name}"
    else
        echo "  ✗ Failed to build ${dir_name}" >&2
        exit 1
    fi
done

echo ""
echo "All kustomizations built successfully!"
echo "Output directory: ${OUTPUT_DIR}"
