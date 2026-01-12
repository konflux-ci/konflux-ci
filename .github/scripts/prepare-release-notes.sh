#!/bin/bash
set -euo pipefail

# Prepare Release Notes Script
# This script generates release notes in markdown format for GitHub releases
#
# Usage:
#   prepare-release-notes.sh <version> <image_tag> <git_ref> <output_file>
#
# Arguments:
#   version     - Release version (e.g., v0.2025.03)
#   image_tag   - Image tag (e.g., release-sha-abc1234)
#   git_ref     - Git ref (commit SHA, branch, or tag)
#   output_file - Path to output file
#
# Example:
#   prepare-release-notes.sh v0.2025.03 release-sha-abc1234 c515b60f474cb00a11176e5b400205a679b68aac /tmp/release-notes.md
#   prepare-release-notes.sh v0.2025.03 release-sha-abc1234 main /tmp/release-notes.md

if [ $# -ne 4 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <version> <image_tag> <git_ref> <output_file>"
  exit 1
fi

VERSION="$1"
IMAGE_TAG="$2"
GIT_REF="$3"
OUTPUT_FILE="$4"

# Generate release notes
cat > "${OUTPUT_FILE}" <<EOF
## Release ${VERSION}

### Installation
\`\`\`bash
kubectl apply -f https://github.com/konflux-ci/konflux-ci/releases/download/${VERSION}/install.yaml
\`\`\`

### Image
- **Repository**: quay.io/konflux-ci/konflux-operator
- **Tag**: ${IMAGE_TAG}
- **Git Ref**: ${GIT_REF}
- **Pull command**: \`podman pull quay.io/konflux-ci/konflux-operator:${IMAGE_TAG}\`

### Artifacts
- install.yaml - Complete installation manifest (includes CRDs, RBAC, and operator deployment)
- samples.tar.gz - Sample Custom Resources

### Documentation
- [README.md](https://github.com/konflux-ci/konflux-ci/blob/main/README.md) - Installation and usage instructions
EOF

echo "Release notes generated at: ${OUTPUT_FILE}"
