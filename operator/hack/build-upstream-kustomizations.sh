#!/bin/bash
#
# Build all kustomizations in upstream-kustomizations directory
# and place the results in built-upstream-kustomizations directory
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_DIR="${SCRIPT_DIR}/../upstream-kustomizations"
OUTPUT_DIR="${SCRIPT_DIR}/../built-upstream-kustomizations"

# Clean and create output directory
rm -rf "${OUTPUT_DIR}"
mkdir -p "${OUTPUT_DIR}"

# Iterate through each directory in upstream-kustomizations
for dir in "${SOURCE_DIR}"/*/; do
    dir_name="$(basename "${dir}")"
    output_subdir="${OUTPUT_DIR}/${dir_name}"

    echo "Building ${dir_name}..."
    mkdir -p "${output_subdir}"

    if kustomize build "${dir}" > "${output_subdir}/manifests.yaml"; then
        echo "  ✓ Successfully built ${dir_name}"
    else
        echo "  ✗ Failed to build ${dir_name}" >&2
        exit 1
    fi
done

echo ""
echo "All kustomizations built successfully!"
echo "Output directory: ${OUTPUT_DIR}"

