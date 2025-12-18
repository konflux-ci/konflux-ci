#!/bin/bash
set -euo pipefail

# Kube-linter runner script
# This script handles manifest generation, kube-linter installation, and scanning
# The kube-linter version is tracked by Renovate for automatic updates

# renovate: datasource=github-releases depName=stackrox/kube-linter
KUBE_LINTER_VERSION="0.7.6"

# Cleanup function for temporary files
cleanup() {
  echo "Cleaning up temporary files..."
  rm -f dependencies/smee/smee-channel-id.yaml
}

# Set up trap to cleanup on exit
trap cleanup EXIT

echo "=== Kube-linter CI Script ==="
echo "Using kube-linter version: ${KUBE_LINTER_VERSION}"

# Step 1: Create output directory
echo "Creating .kube-linter directory..."
mkdir -p ./.kube-linter/

# Step 2: Generate manifests
echo "Generating Kubernetes manifests..."

# workaround: create dummy patch for building the manifests for kube-linter
echo "Creating temporary smee-channel-id.yaml for manifest generation..."
cp dependencies/smee/smee-channel-id.tpl dependencies/smee/smee-channel-id.yaml

# Exclude operator directory from kustomize builds
find . \( -name "kustomization.yaml" -o -name "kustomization.yml" \) ! -path "*/operator/*" | while read -r file; do
  dir=$(dirname "$file")
  dir=${dir#./}
  output_file=$(echo "out-$dir" | tr "/" "-")
  echo "  Building $dir -> .kube-linter/$output_file.yaml"
  kustomize build "$dir" > "./.kube-linter/$output_file.yaml"
done

# Copy pre-built operator manifests to .kube-linter directory
echo "Copying pre-built operator manifests to .kube-linter/operator-manifests..."
cp -r operator/pkg/manifests ./.kube-linter/operator-manifests

echo "Manifest generation completed."

# Step 3: Install kube-linter
echo "Installing kube-linter version ${KUBE_LINTER_VERSION}..."

# Download URL for Linux (GitHub Actions runner)
DOWNLOAD_URL="https://github.com/stackrox/kube-linter/releases/download/v${KUBE_LINTER_VERSION}/kube-linter-linux.tar.gz"
ARCHIVE_FILE="kube-linter-linux.tar.gz"

echo "Downloading from: ${DOWNLOAD_URL}"
wget -q "${DOWNLOAD_URL}" -O "${ARCHIVE_FILE}"

echo "Extracting kube-linter binary"
tar -xzf "${ARCHIVE_FILE}"

echo "Installing kube-linter to /usr/local/bin/"
sudo mv kube-linter /usr/local/bin/

echo "Cleaning up download files"
rm -f "${ARCHIVE_FILE}"

echo "Verifying kube-linter installation"
which kube-linter
kube-linter version

# Step 4: Run kube-linter scan
echo "Running kube-linter scan..."
echo "Config file: ./.github/.kube-linter-config.yaml"
echo "Scan directory: ./.kube-linter/"

# Show some debug info
echo "Files to be scanned:"
find ./.kube-linter -name "*.yaml" -o -name "*.yml" | head -5
echo ""

# Run the scan with verbose output
kube-linter lint ./.kube-linter/ --config ./.github/.kube-linter-config.yaml --verbose

echo "=== Kube-linter scan completed successfully! ==="
