#!/bin/bash
set -euo pipefail

# Generate Release Artifacts Script
# This script generates all release artifacts:
#   - version: plain text file with the release version
#   - install.yaml: Kubernetes manifests for installation
#   - samples.tar.gz: sample CR configurations
#   - bundle.tar.gz: OLM bundle with Dockerfile and release-config.yaml
# Must be run from the operator directory
#
# Usage:
#   generate-release-artifacts.sh <image_tag> <version>
#
# Arguments:
#   image_tag - Image tag for the operator (e.g., release-sha-abc1234)
#   version   - Release version (e.g., v0.0.1 or 0.0.1)
#
# Example:
#   generate-release-artifacts.sh release-sha-abc1234 v0.0.1

if [ $# -ne 2 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <image_tag> <version>"
  exit 1
fi

IMAGE_TAG="$1"
VERSION="$2"
# Ensure version has 'v' prefix for the version file
VERSION_WITH_V="${VERSION}"
if [[ ! "${VERSION_WITH_V}" =~ ^v ]]; then
  VERSION_WITH_V="v${VERSION_WITH_V}"
fi
# Strip 'v' prefix if present for VERSION (Makefile expects version without 'v')
VERSION="${VERSION#v}"
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

# Generate version file
echo "Generating version file..."
echo "${VERSION_WITH_V}" > dist/version
echo "✅ Generated version file at: $(pwd)/dist/version"
cat dist/version

# Generate install.yaml
echo "Generating install.yaml..."
make build-installer IMG="${IMG}"
echo "✅ Generated install.yaml at: $(pwd)/dist/install.yaml"
ls -lh dist/install.yaml

# Package samples
echo "Packaging samples..."
pushd config/samples
tar czf ../../dist/samples.tar.gz ./*.yaml
popd
echo "✅ Generated samples.tar.gz at: $(pwd)/dist/samples.tar.gz"
ls -lh dist/samples.tar.gz

# Generate OLM bundle
echo "Generating OLM bundle..."
make bundle IMG="${IMG}" VERSION="${VERSION}"
echo "✅ Generated OLM bundle at: $(pwd)/bundle"

# Generate release-config.yaml (for OLM catalog configuration)
# https://redhat-openshift-ecosystem.github.io/operator-pipelines/users/fbc_autorelease/
echo "Generating release-config.yaml..."
cat > release-config.yaml << 'EOF'
---
catalog_templates:
  - template_name: semver.yaml
    channels: [Stable]
EOF
echo "✅ Generated release-config.yaml"
cat release-config.yaml

# Package bundle (includes bundle/ directory contents, bundle.Dockerfile, and release-config.yaml)
echo "Packaging bundle..."
tar czf dist/bundle.tar.gz -C bundle . -C .. bundle.Dockerfile release-config.yaml
rm release-config.yaml
echo "✅ Generated bundle.tar.gz at: $(pwd)/dist/bundle.tar.gz"
ls -lh dist/bundle.tar.gz

echo ""
echo "All release artifacts generated successfully:"
ls -lh dist/
