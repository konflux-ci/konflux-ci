#!/usr/bin/env bash
#
# Resolve Konflux CR file path
#
# Determines which Konflux CR to apply based on environment variables
# and prints the resolved path to stdout.
#
# Precedence (high to low):
#   1. KONFLUX_CR environment variable (if already set)
#   2. Auto-select konflux-e2e.yaml when QUAY_TOKEN and QUAY_ORGANIZATION are set
#   3. Default: konflux_v1alpha1_konflux.yaml
#
# Optional:
#   SAMPLES_DIR - Directory containing sample CRs (default: operator/config/samples).
#                 Used by deploy-local.sh release mode to point at release samples.
#
# Usage:
#   KONFLUX_CR=$(./scripts/resolve-konflux-cr.sh)

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")
SAMPLES_DIR="${SAMPLES_DIR:-${REPO_ROOT}/operator/config/samples}"

if [ -n "${KONFLUX_CR:-}" ]; then
    # Explicit CR specified — use it as-is
    true
elif [ -n "${QUAY_TOKEN:-}" ] && [ -n "${QUAY_ORGANIZATION:-}" ]; then
    KONFLUX_CR="${SAMPLES_DIR}/konflux-e2e.yaml"
    echo "INFO: Auto-selecting konflux-e2e.yaml because QUAY_TOKEN/QUAY_ORGANIZATION are set" >&2
    echo "      This CR enables image-controller required for Quay integration" >&2
    echo "      To use a different CR, set KONFLUX_CR environment variable" >&2
else
    KONFLUX_CR="${SAMPLES_DIR}/konflux_v1alpha1_konflux.yaml"
fi

# Convert relative path to absolute
if [[ "${KONFLUX_CR}" != /* ]]; then
    KONFLUX_CR="${REPO_ROOT}/${KONFLUX_CR}"
fi

if [ ! -f "${KONFLUX_CR}" ]; then
    echo "ERROR: Konflux CR file not found: ${KONFLUX_CR}" >&2
    exit 1
fi

echo "${KONFLUX_CR}"
