#!/usr/bin/env bash
# Set RELEASE_SERVICE_CATALOG_REVISION and CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE.
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

line="$(grep 'RELEASE_SERVICE_CATALOG_REVISION=' test/e2e/release-service-catalog-revision | sed 's/^export //')"
append_env "$line"

BUNDLE="$(yq eval-all 'select(.kind == "ConfigMap" and .metadata.name == "build-pipeline-config") | .data["config.yaml"]' operator/pkg/manifests/build-service/manifests.yaml | yq eval '.pipelines[] | select(.name == "docker-build-oci-ta-min") | .bundle' -)"
append_env "CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE=${BUNDLE}"
