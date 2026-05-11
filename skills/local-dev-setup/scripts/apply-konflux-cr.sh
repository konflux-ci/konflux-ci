#!/usr/bin/env bash
set -euo pipefail

# Apply the Konflux CR to the cluster.
#
# Resolves the CR path using scripts/resolve-konflux-cr.sh (same logic as
# deploy-local.sh). Pass an explicit path to override.
#
# Usage:
#   bash skills/local-dev-setup/scripts/apply-konflux-cr.sh [cr-path]

REPO_ROOT=$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)

CR="${1:-}"

if [ -n "${CR}" ]; then
    export KONFLUX_CR="${CR}"
fi

CR=$("${REPO_ROOT}/scripts/resolve-konflux-cr.sh")

echo "Applying Konflux CR: ${CR}"
kubectl apply -f "${CR}"
