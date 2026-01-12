#!/bin/bash
set -euo pipefail

# Detect SHA from Quay Script
# This script detects the latest image tag from Quay and maps the short SHA to full commit SHA.
# If image_tag or commit_sha (git ref) are provided, they are used (with validation).
# Otherwise, values are auto-detected from Quay.
#
# Usage:
#   detect-sha-from-quay.sh [repository_root] [image_tag] [commit_sha]
#
# Arguments:
#   repository_root - Optional path to repository root (default: current directory)
#   image_tag       - Optional image tag (e.g., release-sha-abc1234). If not provided, will detect from Quay
#   commit_sha      - Optional git ref (commit SHA, branch, or tag). If not provided, will detect from Quay
#
# Outputs (to stdout, key=value format):
#   image_tag=value
#   git_ref=value
#
# Example:
#   detect-sha-from-quay.sh
#   detect-sha-from-quay.sh . release-sha-abc1234
#   detect-sha-from-quay.sh . "" c32df7f1234567890abcdef1234567890abcdef12

REPO_ROOT="${1:-.}"
PROVIDED_IMAGE_TAG="${2:-}"
PROVIDED_GIT_REF="${3:-}"
REPO_NAMESPACE="konflux-ci"
REPO_NAME="konflux-operator"
QUAY_API_URL="https://quay.io/api/v1"

cd "${REPO_ROOT}"

# If both are provided, validate and use them
if [ -n "${PROVIDED_IMAGE_TAG}" ] && [ -n "${PROVIDED_GIT_REF}" ]; then
    echo "Using provided values" >&2

    # Validate git ref exists (can be SHA, branch, or tag)
    if ! git rev-parse --verify "${PROVIDED_GIT_REF}" >/dev/null 2>&1; then
        echo "Error: Provided git ref not found: ${PROVIDED_GIT_REF}" >&2
        exit 1
    fi

    echo "image_tag=${PROVIDED_IMAGE_TAG}"
    echo "git_ref=${PROVIDED_GIT_REF}"
    exit 0
fi

# Determine image_tag: use provided or detect from Quay
if [ -n "${PROVIDED_IMAGE_TAG}" ]; then
    IMAGE_TAG="${PROVIDED_IMAGE_TAG}"
    echo "Using provided image_tag: ${IMAGE_TAG}" >&2
else
    # Need to detect image tag from Quay
    echo "Detecting latest image from Quay..." >&2

    # Query Quay API for tags matching release-sha-* pattern
    # Get tags, filter for release-sha-*, sort by timestamp (latest first), get the first one
    latest_tag=$(curl -s "${QUAY_API_URL}/repository/${REPO_NAMESPACE}/${REPO_NAME}/tag/?limit=100&onlyActiveTags=true" \
        | jq -r '[.tags[] | select(.name | startswith("release-sha-"))] | sort_by(.start_ts) | reverse | .[0].name // empty' 2>/dev/null || echo "")

    if [ -z "${latest_tag}" ]; then
        echo "Error: No release-sha-* tags found in Quay repository ${REPO_NAMESPACE}/${REPO_NAME}" >&2
        exit 1
    fi

    IMAGE_TAG="${latest_tag}"
    echo "Using detected image_tag: ${IMAGE_TAG}" >&2
fi

# If git ref is provided, validate it
if [ -n "${PROVIDED_GIT_REF}" ]; then
    GIT_REF="${PROVIDED_GIT_REF}"
    echo "Using provided git ref: ${GIT_REF}" >&2

    # Validate git ref exists (can be SHA, branch, or tag)
    if ! git rev-parse --verify "${GIT_REF}" >/dev/null 2>&1; then
        echo "Error: Provided git ref not found: ${GIT_REF}" >&2
        exit 1
    fi
else
    # Need to extract short SHA from image tag to map to full commit SHA
    # Extract short SHA from image tag (e.g., release-sha-c32df7f -> c32df7f)
    short_sha="${IMAGE_TAG#release-sha-}"

    if [ -z "${short_sha}" ]; then
        echo "Error: Could not extract SHA from image tag: ${IMAGE_TAG}" >&2
        exit 1
    fi

    echo "Extracted short SHA: ${short_sha}" >&2

    # Map short SHA to full commit SHA using git rev-parse
    full_sha=$(git rev-parse "${short_sha}" 2>/dev/null || echo "")

    if [ -z "${full_sha}" ]; then
        # If rev-parse fails, try to fetch and search
        echo "Short SHA not found locally, attempting to fetch..." >&2
        git fetch origin --depth=100 2>/dev/null || true

        # Try again after fetch
        full_sha=$(git rev-parse "${short_sha}" 2>/dev/null || echo "")
    fi

    if [ -z "${full_sha}" ]; then
        echo "Error: Could not resolve short SHA ${short_sha} to full commit SHA" >&2
        echo "Make sure the repository is checked out and the commit exists" >&2
        exit 1
    fi

    # Verify it's a valid full SHA (40 characters for SHA-1)
    # This validation is needed here because we're extracting from release-sha-* tags
    if [ ${#full_sha} -ne 40 ]; then
        echo "Error: Resolved SHA is not a full commit SHA: ${full_sha}" >&2
        exit 1
    fi

    GIT_REF="${full_sha}"
    echo "Mapped to full SHA: ${GIT_REF}" >&2
fi

# Output in key=value format for easy parsing
echo "image_tag=${IMAGE_TAG}"
echo "git_ref=${GIT_REF}"
