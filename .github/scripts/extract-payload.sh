#!/usr/bin/env bash
# extract-payload.sh - Extract version, image_tag, and git_ref from workflow event
#
# This script extracts release information from either:
# - repository_dispatch event (automated from Konflux): reads from github.event.client_payload
# - workflow_dispatch event (manual): reads from workflow inputs
#
# Outputs to stdout in format: key=value (one per line)
# Outputs to GITHUB_OUTPUT if GITHUB_OUTPUT is set

set -euo pipefail

EVENT_NAME="${1:-}"
VERSION_INPUT="${2:-}"
IMAGE_TAG_INPUT="${3:-}"
GIT_REF_INPUT="${4:-}"
VERSION_PAYLOAD="${5:-}"
IMAGE_TAG_PAYLOAD="${6:-}"
GIT_REF_PAYLOAD="${7:-}"

if [ -z "${EVENT_NAME}" ]; then
  echo "Usage: extract-payload.sh <event_name> [version_input] [image_tag_input] [git_ref_input] [version_payload] [image_tag_payload] [git_ref_payload]"
  exit 1
fi

if [ "${EVENT_NAME}" == "repository_dispatch" ]; then
  # Extract from repository_dispatch payload (automated from Konflux)
  VERSION="${VERSION_PAYLOAD}"
  IMAGE_TAG="${IMAGE_TAG_PAYLOAD}"
  GIT_REF="${GIT_REF_PAYLOAD}"
  SOURCE="repository_dispatch (Konflux)"
else
  # Extract from workflow_dispatch inputs (manual)
  VERSION="${VERSION_INPUT}"
  IMAGE_TAG="${IMAGE_TAG_INPUT}"
  GIT_REF="${GIT_REF_INPUT}"
  SOURCE="workflow_dispatch (manual)"
fi

# Validate required inputs
if [ -z "${VERSION}" ]; then
  echo "Error: version is required but not provided" >&2
  exit 1
fi
if [ -z "${GIT_REF}" ]; then
  echo "Error: git_ref is required but not provided" >&2
  exit 1
fi
if [ -z "${IMAGE_TAG}" ]; then
  echo "Error: image_tag is required but not provided" >&2
  exit 1
fi

echo "All required inputs validated:" >&2
echo "  version: ${VERSION}" >&2
echo "  git_ref: ${GIT_REF}" >&2
echo "  image_tag: ${IMAGE_TAG}" >&2

# Output values
echo "version=${VERSION}"
echo "image_tag=${IMAGE_TAG}"
echo "git_ref=${GIT_REF}"
echo "source=${SOURCE}"

# Also output to GITHUB_OUTPUT if set
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  {
    echo "version=${VERSION}"
    echo "image_tag=${IMAGE_TAG}"
    echo "git_ref=${GIT_REF}"
    echo "source=${SOURCE}"
  } >> "${GITHUB_OUTPUT}"
fi
