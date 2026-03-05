#!/bin/bash
# shellcheck source-path=SCRIPTDIR
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

if [ "$#" -ne 4 ]; then
  echo "Error: Invalid number of arguments" >&2
  echo "Usage: $0 <version> <image_tag> <git_ref> <output_file>" >&2
  exit 1
fi

VERSION="$1"
IMAGE_TAG="$2"
GIT_REF="$3"
OUTPUT_FILE="$4"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091 source=changelog-lib.sh
source "${SCRIPT_DIR}/changelog-lib.sh"

# generate_operator_changelog generates a curated "## Changes" section from
# conventional commits in the operator repo between two refs.
# Args: $1=base_ref, $2=head_ref
# Output: Markdown to stdout (empty if no conventional commits found)
generate_operator_changelog() {
  local base_ref="$1"
  local head_ref="$2"

  local encoded_base encoded_head
  encoded_base=$(printf '%s' "$base_ref" | jq -sRr @uri)
  encoded_head=$(printf '%s' "$head_ref" | jq -sRr @uri)

  local api_response
  api_response=$(gh api "repos/konflux-ci/konflux-ci/compare/${encoded_base}...${encoded_head}" \
    --header "Accept: application/vnd.github+json" 2>/dev/null) || {
    echo "  Warning: GitHub API call failed for operator changelog" >&2
    return 1
  }

  local filtered_commits
  filtered_commits=$(echo "$api_response" | changelog_filter_commits 2>/dev/null) || {
    echo "  Warning: Failed to filter operator commits" >&2
    return 1
  }

  if [ -z "$filtered_commits" ]; then
    echo "  No conventional commits found in operator repo" >&2
    return 0
  fi

  echo "## Changes"
  echo ""
  echo "$filtered_commits" | changelog_format_grouped

  local count
  count=$(echo "$filtered_commits" | wc -l | tr -d ' ')
  echo "  Found ${count} operator conventional commit(s)" >&2
}

# get_comparison_base finds the commit to compare against for changelog generation.
# First finds the highest previous stable release tag (semantic version sort,
# strict vX.Y.Z regex excluding pre-release suffixes). Then returns the common
# ancestor (merge-base) between that tag and GIT_REF.
#
# This handles both release models correctly:
# - Patch releases (z>0): merge-base of two commits on the same branch returns
#   the older one, equivalent to using the tag directly.
# - Minor releases (z=0): merge-base returns the divergence point between
#   release branches, capturing all changes since the branch fork — including
#   feats/fixes that were also backported to patch releases on the old branch.
#
# Excludes $VERSION so re-runs after tag creation still return the correct base.
get_comparison_base() {
  local prev_tag
  prev_tag=$(git tag --sort=-version:refname 2>/dev/null \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
    | grep -v "^${VERSION}$" \
    | head -1) || true

  if [ -n "$prev_tag" ]; then
    git merge-base "$prev_tag" "$GIT_REF" 2>/dev/null || echo "$prev_tag"
  fi
}

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
- **Git Ref**: [${GIT_REF}](https://github.com/konflux-ci/konflux-ci/tree/${GIT_REF})
- **Pull command**: \`podman pull quay.io/konflux-ci/konflux-operator:${IMAGE_TAG}\`

### Artifacts
- install.yaml - Complete installation manifest (includes CRDs, RBAC, and operator deployment)
- samples.tar.gz - Sample Custom Resources

### Documentation
- [Documentation](https://konflux-ci.dev/konflux-ci/) - Installation and usage instructions
EOF

# Append changelogs (failures here must never block the release)
COMPARISON_BASE=$(get_comparison_base)
if [ -n "$COMPARISON_BASE" ]; then
  # Operator changelog: curated feat/fix commits from this repo
  echo "Generating operator changelog: ${COMPARISON_BASE} -> ${GIT_REF}" >&2
  operator_changelog=$(generate_operator_changelog "$COMPARISON_BASE" "$GIT_REF") || true
  if [ -n "$operator_changelog" ]; then
    printf '\n%s\n' "$operator_changelog" >> "${OUTPUT_FILE}"
  fi

  # Upstream changelog: curated feat/fix commits from upstream component repos
  echo "Generating upstream changelog: ${COMPARISON_BASE} -> ${GIT_REF}" >&2
  changelog=$("${SCRIPT_DIR}/generate-changelog.sh" "$COMPARISON_BASE" "$GIT_REF") || true
  if [ -n "$changelog" ]; then
    printf '\n%s\n' "$changelog" >> "${OUTPUT_FILE}"
  fi
else
  echo "No comparison base found, skipping changelogs" >&2
fi

echo "Release notes generated at: ${OUTPUT_FILE}" >&2
