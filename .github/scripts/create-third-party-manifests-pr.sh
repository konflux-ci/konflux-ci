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
#   GH_TOKEN                    - Used for push and gh pr (workflow provides this)
#   CERT_MANAGER_VERSION        - Pin from export-third-party-chart-env.sh (for messages)
#   TRUST_MANAGER_VERSION       - Pin from export-third-party-chart-env.sh (for messages)
#   PROMETHEUS_OPERATOR_VERSION - Pin from export-third-party-chart-env.sh (for messages)
#   DRY_RUN                     - If set, do not push or create PR (useful for local testing)
#
# In GitHub Actions: creates branch update-third-party-manifests, commits, pushes, creates/updates PR.
# Locally (GITHUB_ACTIONS != true): skips git config; with DRY_RUN skips push and PR.

REPO_ROOT="${1:-${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:?CERT_MANAGER_VERSION is required}"
TRUST_MANAGER_VERSION="${TRUST_MANAGER_VERSION:?TRUST_MANAGER_VERSION is required}"
PROMETHEUS_OPERATOR_VERSION="${PROMETHEUS_OPERATOR_VERSION:?PROMETHEUS_OPERATOR_VERSION is required}"
BRANCH_NAME="update-third-party-manifests"

readonly -a THIRD_PARTY_PATHS=(
  dependencies/cert-manager/cert-manager.yaml
  dependencies/trust-manager/trust-manager.yaml
  operator/test/crds/cert-manager/cert-manager.crds.yaml
  operator/test/crds/prometheus/servicemonitors.monitoring.coreos.com.yaml
  dependencies/prometheus-operator-crds/servicemonitors.monitoring.coreos.com.yaml
)

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

changed_paths=()
for path in "${THIRD_PARTY_PATHS[@]}"; do
  if ! git diff --quiet -- "$path"; then
    changed_paths+=("$path")
  fi
done

if [ "${#changed_paths[@]}" -eq 0 ]; then
  echo "No changes to third-party manifests or envtest CRDs."
  exit 0
fi

cert_manager_changed=false
trust_manager_changed=false
prometheus_changed=false
for path in "${changed_paths[@]}"; do
  case "$path" in
    dependencies/cert-manager/*|operator/test/crds/cert-manager/*)
      cert_manager_changed=true
      ;;
    dependencies/trust-manager/*)
      trust_manager_changed=true
      ;;
    *prometheus*)
      prometheus_changed=true
      ;;
  esac
done

commit_bullets=()
pr_versions=()
if [ "$cert_manager_changed" = true ]; then
  commit_bullets+=("- cert-manager: ${CERT_MANAGER_VERSION} (Helm)")
  pr_versions+=("- **cert-manager:** ${CERT_MANAGER_VERSION}")
fi
if [ "$trust_manager_changed" = true ]; then
  commit_bullets+=("- trust-manager: ${TRUST_MANAGER_VERSION} (Helm)")
  pr_versions+=("- **trust-manager:** ${TRUST_MANAGER_VERSION}")
fi
if [ "$prometheus_changed" = true ]; then
  commit_bullets+=("- prometheus-operator ServiceMonitor CRD: ${PROMETHEUS_OPERATOR_VERSION}")
  pr_versions+=("- **prometheus-operator ServiceMonitor CRD:** ${PROMETHEUS_OPERATOR_VERSION}")
fi

commit_body="$(printf '%s\n' "${commit_bullets[@]}")"
pr_version_block="$(printf '%s\n' "${pr_versions[@]}")"

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

git add "${changed_paths[@]}"
git commit -m "$(cat <<EOF
chore(deps): update third-party manifests

${commit_body}
EOF
)"

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
PR_TITLE="chore(deps): update third-party manifests"
PR_BODY="$(cat <<EOF
## Third-Party Manifests Update

${pr_version_block}

---
*This PR was automatically created by the [Update Third-Party Manifests workflow](.github/workflows/update-third-party-manifests.yaml)*
EOF
)"

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
