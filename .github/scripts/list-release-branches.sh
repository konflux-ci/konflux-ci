#!/bin/bash
set -euo pipefail

# List main and release-x.y branches that are not in the excluded list.
# Outputs a JSON array of branch names (for workflow matrix).
# Used by auto-tag-weekly and by release verification workflow.
#
# Usage: .github/scripts/list-release-branches.sh
# Expects: run from repository root; yq and jq available.
# EXCLUDED_BRANCHES_FILE defaults to .github/excluded-release-branches.yaml

if ! command -v yq &>/dev/null; then
  echo "yq is required but not installed" >&2
  exit 1
fi

EXCLUDED_FILE="${EXCLUDED_BRANCHES_FILE:-.github/excluded-release-branches.yaml}"
REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

# All remote release-x.y branches (strip origin/)
ALL_BRANCHES=$(git branch -r 2>/dev/null | sed 's/^[[:space:]]*//' | grep -E '^origin/release-[0-9]+\.[0-9]+$' | sed 's|^origin/||' | sort -V || true)

# Excluded set from YAML
EXCLUDED=""
if [ -f "$EXCLUDED_FILE" ]; then
  EXCLUDED=$(yq -r '.excluded[]?' "$EXCLUDED_FILE" 2>/dev/null || true)
fi

{
  echo "main"
  while IFS= read -r br; do
    [ -z "$br" ] && continue
    echo "$EXCLUDED" | grep -Fxq "$br" 2>/dev/null && continue
    echo "$br"
  done <<< "$ALL_BRANCHES"
} | jq -R -s -c 'split("\n") | map(select(length>0))'
