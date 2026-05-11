#!/usr/bin/env bash
set -euo pipefail

# Wait for the Konflux CR to become Ready.
#
# Usage:
#   bash skills/local-dev-setup/scripts/wait-for-konflux.sh [timeout]
#
# Default timeout: 15m

TIMEOUT="${1:-15m}"

echo "Waiting for Konflux to become ready (timeout: ${TIMEOUT})..."
if kubectl wait --for=condition=Ready=True konflux konflux --timeout="${TIMEOUT}"; then
    echo "Konflux is ready."
else
    echo "ERROR: Konflux did not become Ready within ${TIMEOUT}" >&2
    echo "Debug with:" >&2
    echo "  kubectl get konflux konflux -o yaml" >&2
    exit 1
fi
