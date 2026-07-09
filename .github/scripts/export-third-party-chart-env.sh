#!/usr/bin/env bash
# Print shell `export` lines for CERT_MANAGER_VERSION, TRUST_MANAGER_VERSION, and
# PROMETHEUS_OPERATOR_VERSION (Helm --version values with a leading v where applicable).
# Chart semver pins live in this script and are updated by Renovate / MintMaker (see renovate.json).
#
# Usage (from repository root, or pass the full path to this script):
#   eval "$(bash .github/scripts/export-third-party-chart-env.sh)"
set -euo pipefail

# renovate: datasource=helm depName=jetstack/cert-manager versioning=semver
CERT_MANAGER_CHART_VERSION="1.20.3"
# renovate: datasource=helm depName=jetstack/trust-manager versioning=semver
TRUST_MANAGER_CHART_VERSION="0.24.0"
# renovate: datasource=github-releases depName=prometheus-operator/prometheus-operator versioning=semver
PROMETHEUS_OPERATOR_VERSION="0.92.1"

cert="${CERT_MANAGER_CHART_VERSION}"
trust="${TRUST_MANAGER_CHART_VERSION}"
prometheus="${PROMETHEUS_OPERATOR_VERSION}"

if [[ -z "${cert}" || -z "${trust}" || -z "${prometheus}" ]]; then
  echo "export-third-party-chart-env: chart versions are not set in ${BASH_SOURCE[0]}" >&2
  exit 1
fi

[[ "${cert}" == v* ]] || cert="v${cert}"
[[ "${trust}" == v* ]] || trust="v${trust}"
[[ "${prometheus}" == v* ]] || prometheus="v${prometheus}"

printf 'export CERT_MANAGER_VERSION=%q\n' "${cert}"
printf 'export TRUST_MANAGER_VERSION=%q\n' "${trust}"
printf 'export PROMETHEUS_OPERATOR_VERSION=%q\n' "${prometheus}"
