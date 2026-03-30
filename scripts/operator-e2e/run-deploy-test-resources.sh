#!/usr/bin/env bash
# Run deploy-test-resources.sh from repo root. Honors SKIP_SAMPLE_COMPONENTS (default true for e2e).
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
export SKIP_SAMPLE_COMPONENTS="${SKIP_SAMPLE_COMPONENTS:-true}"

cd "$REPO_ROOT"
./deploy-test-resources.sh
