#!/bin/bash
set -euo pipefail

# If third-party manifest files changed, create a branch and open (or update) a PR.
# Used by the Update Third-Party Manifests workflow. Does not push to main.
#
# Usage:
#   create-third-party-manifests-pr.sh [REPO_ROOT]
#
# Arguments:
#   REPO_ROOT - Repository root (default: GITHUB_WORKSPACE or git rev-parse --show-toplevel)
#
# Environment:
#   GH_TOKEN               - Used for push and gh pr (workflow provides this)
#   CERT_MANAGER_VERSION   - Required. For commit/PR message.
#   TRUST_MANAGER_VERSION  - Required. For commit/PR message.
#   DRY_RUN                - If set, do not push or create PR (useful for local testing)
#
# In GitHub Actions: creates branch update-third-party-manifests, commits, pushes, creates/updates PR.
# Locally (GITHUB_ACTIONS != true): skips git config; with DRY_RUN skips push and PR.

REPO_ROOT="${1:-${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:?CERT_MANAGER_VERSION is required}"
TRUST_MANAGER_VERSION="${TRUST_MANAGER_VERSION:?TRUST_MANAGER_VERSION is required}"
BRANCH_NAME="update-third-party-manifests"

cd "$REPO_ROOT"

if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  git config --local user.email "github-actions[bot]@users.noreply.github.com"
  git config --local user.name "github-actions[bot]"
  if [ -n "${GH_TOKEN:-}" ]; then
    git config --local credential.helper store
    echo "https://x-access-token:${GH_TOKEN}@github.com" > ~/.git-credentials
  fi
fi

git checkout main
git fetch origin main

if git diff --quiet -- dependencies/ operator/test/crds/; then
  echo "No changes to third-party manifests or envtest CRDs."
  exit 0
fi

# Delete local branch if it exists
if git show-ref --verify --quiet refs/heads/"${BRANCH_NAME}"; then
  echo "Deleting existing local branch ${BRANCH_NAME}..."
  git branch -D "${BRANCH_NAME}"
fi

# Create branch from main
if git ls-remote --heads origin "${BRANCH_NAME}" | grep -q .; then
  echo "Branch ${BRANCH_NAME} exists remotely, recreating from main..."
  git checkout -b "${BRANCH_NAME}" main
else
  echo "Creating new branch ${BRANCH_NAME}..."
  git checkout -b "${BRANCH_NAME}" main
fi

git add dependencies/cert-manager/cert-manager.yaml dependencies/trust-manager/trust-manager.yaml operator/test/crds/cert-manager/cert-manager.crds.yaml
git commit -m "chore(deps): update cert-manager and trust-manager manifests

- cert-manager: ${CERT_MANAGER_VERSION} (Helm)
- trust-manager: ${TRUST_MANAGER_VERSION} (Helm)
- envtest CRDs: extracted from cert-manager Helm output"

if [ -n "${DRY_RUN:-}" ]; then
  echo "DRY_RUN: would push branch and create/update PR"
  git checkout main
  exit 0
fi

# Push branch
if ! git push origin "${BRANCH_NAME}" --force 2>&1; then
  echo "Failed to push branch ${BRANCH_NAME}" >&2
  git checkout main
  exit 1
fi

# Create or update PR
PR_TITLE="chore(deps): update cert-manager and trust-manager manifests"
PR_BODY="## Third-Party Manifests Update

This PR updates Helm-rendered manifests for cert-manager and trust-manager under \`dependencies/\`.

### Versions
- **cert-manager:** ${CERT_MANAGER_VERSION}
- **trust-manager:** ${TRUST_MANAGER_VERSION}

### What changed?
- \`dependencies/cert-manager/cert-manager.yaml\` – generated with \`helm template\` (OCI chart)
- \`dependencies/trust-manager/trust-manager.yaml\` – generated with \`helm template\` (Jetstack repo)
- \`operator/test/crds/cert-manager/cert-manager.crds.yaml\` – CRDs extracted from cert-manager output for envtest

---
*This PR was automatically created by the [Update Third-Party Manifests workflow](.github/workflows/update-third-party-manifests.yaml)*"

EXISTING_PR=$(gh pr view "${BRANCH_NAME}" \
  --json number,state \
  --jq 'if .number then "\(.number)|\(.state)" else empty end' \
  2>/dev/null || echo "")

if [ -n "${EXISTING_PR}" ]; then
  PR_NUMBER=$(echo "${EXISTING_PR}" | cut -d'|' -f1)
  PR_STATE=$(echo "${EXISTING_PR}" | cut -d'|' -f2)

  if [ "${PR_STATE}" = "CLOSED" ] || [ "${PR_STATE}" = "MERGED" ]; then
    echo "PR #${PR_NUMBER} is ${PR_STATE}, creating new PR..."
  else
    echo "Updating existing PR #${PR_NUMBER}..."
    if gh pr edit "${PR_NUMBER}" --title "${PR_TITLE}" --body "${PR_BODY}" 2>&1; then
      echo "Updated PR #${PR_NUMBER}"
      gh pr edit "${PR_NUMBER}" --add-label "automated,dependencies" 2>/dev/null || true
      git checkout main
      exit 0
    else
      echo "Failed to update PR #${PR_NUMBER}" >&2
      git checkout main
      exit 1
    fi
  fi
fi

# Create new PR
PR_OUTPUT=$(gh pr create \
  --title "${PR_TITLE}" \
  --body "${PR_BODY}" \
  --base main \
  --head "${BRANCH_NAME}" 2>&1) || {
  echo "Failed to create PR: ${PR_OUTPUT}" >&2
  git checkout main
  exit 1
}

PR_NUMBER=$(echo "${PR_OUTPUT}" | sed -n 's|.*pull/\([0-9]\+\).*|\1|p' | head -1)
if [ -z "${PR_NUMBER}" ]; then
  PR_NUMBER=$(gh pr view "${BRANCH_NAME}" --json number --jq .number 2>/dev/null || echo "")
fi
echo "Created PR #${PR_NUMBER}"
gh pr edit "${PR_NUMBER}" --add-label "automated,dependencies" 2>/dev/null || true
git checkout main
exit 0
