#!/bin/bash
set -euo pipefail

# Calculate Version Script
# This script calculates the next release version, either from provided input or
# by auto-incrementing from latest release
#
# Usage:
#   calculate-version.sh [provided_version]
#
# Arguments:
#   provided_version - Optional version to use (takes precedence if provided)
#
# Example:
#   calculate-version.sh v0.0.5
#   calculate-version.sh  # Auto-calculates from latest release

PROVIDED_VERSION="${1:-}"

if [ -n "${PROVIDED_VERSION}" ]; then
    # Use provided version (take precedence)
    VERSION="${PROVIDED_VERSION}"
    echo "Using provided version: ${VERSION}" >&2
else
    # Auto-calculate from latest release
    echo "No version provided, calculating from latest release..." >&2
    latest_tag=$(gh release list --exclude-drafts --limit 1 --json tagName --jq '.[0].tagName' 2>/dev/null || echo "")

    if [ -z "${latest_tag}" ]; then
        echo "Error: No version provided and no existing releases found. Please provide a version for the first release." >&2
        exit 1
    fi

    # Remove 'v' prefix if present
    version="${latest_tag#v}"

    # Extract the last component (patch version) and prefix (everything before last dot)
    patch_version="${version##*.}"
    version_prefix="${version%.*}"

    # Validate patch version is numeric
    if ! [[ "$patch_version" =~ ^[0-9]+$ ]]; then
        echo "Error: Invalid version format in latest release: ${latest_tag} (last component must be numeric)" >&2
        exit 1
    fi

    # Increment patch version while keeping the prefix
    new_patch=$((patch_version + 1))
    VERSION="v${version_prefix}.${new_patch}"
    echo "Calculated version from latest release (${latest_tag}): ${VERSION}" >&2
fi

# Ensure version starts with 'v' prefix
if [[ ! "${VERSION}" =~ ^v ]]; then
    VERSION="v${VERSION}"
fi

# Output version to stdout (for GITHUB_OUTPUT)
echo "${VERSION}"
