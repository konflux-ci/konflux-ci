#!/bin/bash
# configure-hugo-baseurl.sh
# Configures Hugo's baseURL in config.yaml for GitHub Pages deployment
#
# Usage:
#   configure-hugo-baseurl.sh <config_file>
#
# Arguments:
#   config_file: Path to the Hugo config.yaml file (e.g., "operator/docs/config.yaml")
#
# Environment variables:
#   GITHUB_OWNER: GitHub repository owner (e.g., "konflux-ci")
#   REPO_NAME: GitHub repository name (e.g., "konflux-ci")

set -euo pipefail

# Validate required arguments
if [ $# -lt 1 ]; then
  echo "Error: config_file argument is required" >&2
  echo "Usage: configure-hugo-baseurl.sh <config_file>" >&2
  exit 1
fi

CONFIG_FILE="$1"

# Validate required environment variables
if [ -z "${GITHUB_OWNER:-}" ]; then
  echo "Error: GITHUB_OWNER environment variable is not set" >&2
  exit 1
fi

if [ -z "${REPO_NAME:-}" ]; then
  echo "Error: REPO_NAME environment variable is not set" >&2
  exit 1
fi

# Validate config file exists
if [ ! -f "$CONFIG_FILE" ]; then
  echo "Error: Config file '$CONFIG_FILE' not found" >&2
  exit 1
fi

# Calculate baseURL and URL
BASEURL="/${REPO_NAME}/operator"
URL="https://${GITHUB_OWNER}.github.io"
FULL_BASEURL="${URL}${BASEURL}"

echo "Building Hugo site with baseurl=${BASEURL} and url=${URL}"
echo "Full baseURL: ${FULL_BASEURL}"

# Update config.yaml with dynamic values
sed -i "s|^baseURL:.*|baseURL: \"${FULL_BASEURL}\"|" "$CONFIG_FILE"

echo "✓ Updated baseURL in ${CONFIG_FILE}"
