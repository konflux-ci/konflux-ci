#!/bin/bash
set -euo pipefail

# Generate Release Artifacts Script
# This script generates all release artifacts: install.yaml, crds.yaml, rbac.yaml, and samples.tar.gz
# Must be run from the operator directory
#
# Usage:
#   generate-release-artifacts.sh <image_tag>
#
# Arguments:
#   image_tag - Image tag for the operator (e.g., release-sha-abc1234)
#
# Example:
#   generate-release-artifacts.sh release-sha-abc1234

if [ $# -ne 1 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <image_tag>"
  exit 1
fi

IMAGE_TAG="$1"
IMG="quay.io/konflux-ci/konflux-operator:${IMAGE_TAG}"

# Verify we're in the operator directory (or can find it)
if [ ! -f "Makefile" ] && [ -d "operator" ]; then
  cd operator
elif [ ! -f "Makefile" ]; then
  echo "Error: Must be run from operator directory or repository root"
  exit 1
fi

echo "Generating release artifacts with image: ${IMG}"
echo "Working directory: $(pwd)"

# Ensure dist directory exists
mkdir -p dist

# Generate install.yaml
echo "Generating install.yaml..."
make build-installer IMG="${IMG}"
echo "✅ Generated install.yaml at: $(pwd)/dist/install.yaml"
ls -lh dist/install.yaml

# Generate CRDs
echo "Generating crds.yaml..."
make kustomize
./bin/kustomize build config/crd > dist/crds.yaml
echo "✅ Generated crds.yaml at: $(pwd)/dist/crds.yaml"
ls -lh dist/crds.yaml

# Generate RBAC
echo "Generating rbac.yaml..."
./bin/kustomize build config/default/user-rbac > dist/rbac.yaml
echo "✅ Generated rbac.yaml at: $(pwd)/dist/rbac.yaml"
ls -lh dist/rbac.yaml

# Package samples
echo "Packaging samples..."
tar czf dist/samples.tar.gz -C config/samples ./*.yaml
echo "✅ Generated samples.tar.gz at: $(pwd)/dist/samples.tar.gz"
ls -lh dist/samples.tar.gz

echo ""
echo "All release artifacts generated successfully:"
ls -lh dist/
