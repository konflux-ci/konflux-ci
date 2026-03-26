#!/usr/bin/env bash
# Set CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE from build-service manifests
# (same bundle as .github/workflows/operator-test-e2e.yaml "Set build pipeline bundle (min)").
# If GITHUB_ENV is set (GitHub Actions), append KEY=value lines; else print export statements for sourcing.
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
cd "$REPO_ROOT"

append_env() {
  local line="$1"
  if [[ -n "${GITHUB_ENV:-}" ]]; then
    echo "$line" >> "$GITHUB_ENV"
  else
    echo "export $line"
  fi
}

BUNDLE="$(yq eval-all 'select(.kind == "ConfigMap" and .metadata.name == "build-pipeline-config") | .data["config.yaml"]' operator/pkg/manifests/build-service/manifests.yaml | yq eval '.pipelines[] | select(.name == "docker-build-oci-ta-min") | .bundle' -)"
append_env "CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE=${BUNDLE}"
