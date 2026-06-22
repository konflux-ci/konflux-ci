#!/usr/bin/env bash
# Print shell `export` lines for CERT_MANAGER_VERSION and TRUST_MANAGER_VERSION
# (Helm --version values with a leading v). Chart semver pins live in this script and
# are updated by Renovate / MintMaker (see renovate.json customManagers).
#
# Usage (from repository root, or pass the full path to this script):
#   eval "$(bash .github/scripts/export-third-party-chart-env.sh)"
set -euo pipefail

# renovate: datasource=helm depName=jetstack/cert-manager versioning=semver
CERT_MANAGER_CHART_VERSION="1.20.2"
# renovate: datasource=helm depName=jetstack/trust-manager versioning=semver
TRUST_MANAGER_CHART_VERSION="0.21.0"

cert="${CERT_MANAGER_CHART_VERSION}"
trust="${TRUST_MANAGER_CHART_VERSION}"

if [[ -z "${cert}" || -z "${trust}" ]]; then
  echo "export-third-party-chart-env: chart versions are not set in ${BASH_SOURCE[0]}" >&2
  exit 1
fi

[[ "${cert}" == v* ]] || cert="v${cert}"
[[ "${trust}" == v* ]] || trust="v${trust}"

printf 'export CERT_MANAGER_VERSION=%q\n' "${cert}"
printf 'export TRUST_MANAGER_VERSION=%q\n' "${trust}"
