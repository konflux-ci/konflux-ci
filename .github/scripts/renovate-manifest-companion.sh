#!/usr/bin/env bash
# From the current checkout (expected: Renovate PR head), rebuild all upstream
# operator manifests and third-party Helm outputs, commit, push to a stable bot
# branch, and open a PR to main if none exists.
#
# Usage:
#   renovate-manifest-companion.sh [REPO_ROOT]
#
# Environment:
#   SOURCE_PR              - Source dependency PR number (required in CI)
#   GITHUB_REPOSITORY      - owner/repo (required for PR links)
#   GH_TOKEN               - For git push and gh (optional locally)
#
# Chart versions for update-third-party-manifests.sh are read from
# export-third-party-chart-env.sh (inline chart semver pins).
#
# Requires: kustomize, helm, yq, jq, git, gh on PATH (GitHub-hosted runners provide
# the tools used by update-third-party-manifests / kustomize workflows without extra installs).
set -euo pipefail

REPO_ROOT="${1:-${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}}"
REPO_ROOT="$(cd "${REPO_ROOT}" && pwd)"
cd "${REPO_ROOT}"

eval "$(bash "${REPO_ROOT}/.github/scripts/export-third-party-chart-env.sh" "${REPO_ROOT}")"

SOURCE_PR="${SOURCE_PR:?SOURCE_PR (dependency bump PR number) is required}"
GITHUB_REPOSITORY="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

BRANCH="bot/manifest-companion-pr-${SOURCE_PR}"
RENOVATE_PR_URL="https://github.com/${GITHUB_REPOSITORY}/pull/${SOURCE_PR}"

# ---------------------------------------------------------------------------
# Build an enhanced companion PR title that includes a short dependency hint
# derived from the source PR title, falling back to the original pattern.
# ---------------------------------------------------------------------------

# extract_dep_hint <source-pr-title>
#   Strips conventional-commit prefixes, shortens GitHub/registry URLs to the
#   trailing repo/image name, and caps the result so the full companion title
#   stays within a practical length (~120 chars).
extract_dep_hint() {
  local raw="$1"
  # Strip leading conventional-commit scope, e.g. "chore(deps): "
  local hint="${raw}"
  hint="${hint#chore(deps): }"
  hint="${hint#chore: }"
  hint="${hint#fix(deps): }"
  hint="${hint#fix: }"

  # Replace full GitHub URLs with the short repo name
  # e.g. "update https://github.com/konflux-ci/release-service digest"
  #   -> "update release-service digest"
  hint="$(echo "${hint}" | sed -E 's|https?://github\.com/[^/]+/([^/ ]+)|\1|g')"

  # Replace full container-image references with image short name
  # e.g. "update quay.io/konflux-ci/dex docker digest"
  #   -> "update dex docker digest"
  hint="$(echo "${hint}" | sed -E 's|[a-zA-Z0-9._-]+\.[a-z]+/[^: ]+/([^: /]+)(:[^ ]*)?|\1|g')"

  # Trim surrounding whitespace
  hint="$(echo "${hint}" | sed -E 's/^[[:space:]]+|[[:space:]]+$//g')"

  # Cap length; truncate with ellipsis if needed
  local max_hint_len=80
  if (( ${#hint} > max_hint_len )); then
    hint="${hint:0:$((max_hint_len - 1))}…"
  fi

  echo "${hint}"
}

FALLBACK_TITLE="chore: sync manifests (companion to #${SOURCE_PR})"
COMPANION_TITLE="${FALLBACK_TITLE}"

SOURCE_PR_TITLE="$(
  gh pr view "${SOURCE_PR}" \
    --repo "${GITHUB_REPOSITORY}" \
    --json title \
    --jq .title 2>/dev/null || true
)"

if [[ -n "${SOURCE_PR_TITLE}" ]]; then
  DEP_HINT="$(extract_dep_hint "${SOURCE_PR_TITLE}")"
  if [[ -n "${DEP_HINT}" ]]; then
    COMPANION_TITLE="chore: sync rendered manifests (#${SOURCE_PR}: ${DEP_HINT})"
  fi
fi

echo "Companion PR title: ${COMPANION_TITLE}"

if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
  git config --local user.email "github-actions[bot]@users.noreply.github.com"
  git config --local user.name "github-actions[bot]"
  if [[ -n "${GH_TOKEN:-}" ]]; then
    git config --local credential.helper store
    echo "https://x-access-token:${GH_TOKEN}@github.com" >~/.git-credentials
  fi
fi

echo "Creating/updating companion branch ${BRANCH} from $(git rev-parse --short HEAD)"
git checkout -B "${BRANCH}"

bash "${REPO_ROOT}/operator/pkg/manifests/rebuild-upstream-manifests.sh" "${REPO_ROOT}"

export CERT_MANAGER_VERSION TRUST_MANAGER_VERSION
bash "${REPO_ROOT}/.github/scripts/update-third-party-manifests.sh" "${REPO_ROOT}"

git add \
  operator/pkg/manifests \
  dependencies/cert-manager/cert-manager.yaml \
  dependencies/trust-manager/trust-manager.yaml \
  operator/test/crds/cert-manager/cert-manager.crds.yaml

if git diff --cached --quiet; then
  echo "No manifest changes to commit; companion branch matches generators."
  # Stale companion PRs: close if still open. Noop comment: one thread on the source
  # PR, PATCH on repeat so the workflow run link stays current for troubleshooting.
  NOOP_MARKER="<!-- konflux-manifest-companion-noop:${SOURCE_PR} -->"
  if [[ -n "${GH_TOKEN:-}" ]]; then
    # A prior run may have opened a companion PR; this head no longer needs a
    # manifest-only commit. Close the open companion PR so it is not left stale
    # against origin (we do not push the no-diff branch state here).
    OPEN_COMPANION_PR="$(
      gh pr list \
        --repo "${GITHUB_REPOSITORY}" \
        --head "${BRANCH}" \
        --state open \
        --json number \
        --jq '.[0].number // empty' 2>/dev/null || true
    )"
    if [[ -n "${OPEN_COMPANION_PR}" ]]; then
      echo "Closing stale companion PR #${OPEN_COMPANION_PR} (no manifest diff vs generators)."
      gh pr close "${OPEN_COMPANION_PR}" \
        --repo "${GITHUB_REPOSITORY}" \
        --comment "Closed automatically: the latest manifest companion run found no rendered changes to commit for [PR #${SOURCE_PR}](${RENOVATE_PR_URL}); a separate companion PR is not needed. Merge the dependency PR to \`main\` when ready, or reopen if this close was mistaken." \
        || true
    fi
    if [[ -n "${GITHUB_RUN_ID:-}" ]]; then
      NOOP_RUN_LINK="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}"
    else
      NOOP_RUN_LINK="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY}/actions"
    fi
    NOOP_BODY="${NOOP_MARKER}

Manifest companion run found **no staged diff** after regenerating \`operator/pkg/manifests\` and third-party Helm outputs — a companion PR was not opened because rendered paths already match this branch.

If you expected manifest changes, check the [workflow run logs](${NOOP_RUN_LINK}) and the rendering scripts (\`rebuild-upstream-manifests.sh\`, \`update-third-party-manifests.sh\`)."
    NOOP_COMMENT_ID="$(
      gh api --paginate "repos/${GITHUB_REPOSITORY}/issues/${SOURCE_PR}/comments" \
        --jq ".[] | select(.body | contains(\"<!-- konflux-manifest-companion-noop:${SOURCE_PR} -->\")) | .id" 2>/dev/null | tail -n1 || true
    )"
    if [[ -n "${NOOP_COMMENT_ID}" ]]; then
      jq -n --arg body "${NOOP_BODY}" '{body: $body}' \
        | gh api --method PATCH "repos/${GITHUB_REPOSITORY}/issues/comments/${NOOP_COMMENT_ID}" --input - || true
    else
      gh pr comment "${SOURCE_PR}" --repo "${GITHUB_REPOSITORY}" --body "${NOOP_BODY}" || true
    fi
  fi
  exit 0
fi

git commit -m "${COMPANION_TITLE}

Regenerated operator/pkg/manifests and Helm-rendered third-party files to match
the dependency bump branch. Prefer merging this PR over #${SOURCE_PR} so rendered
artifacts stay aligned with pins."

git push origin "${BRANCH}" --force

EXISTING="$(
  gh pr list \
    --repo "${GITHUB_REPOSITORY}" \
    --head "${BRANCH}" \
    --state open \
    --json number \
    --jq '.[0].number // empty' 2>/dev/null || true
)"

BODY_FILE="$(mktemp)"
trap 'rm -f "${BODY_FILE}"' EXIT
cat >"${BODY_FILE}" <<EOF
## Manifest companion for PR #${SOURCE_PR}

This branch is **[${BRANCH}](https://github.com/${GITHUB_REPOSITORY}/tree/${BRANCH})** — it starts from the head of [PR #${SOURCE_PR}](${RENOVATE_PR_URL}) and adds regenerated:

- \`operator/pkg/manifests/*/manifests.yaml\` (from \`kustomize build\` on \`operator/upstream-kustomizations/*\`)
- Third-party Helm outputs under \`dependencies/\` and envtest CRDs under \`operator/test/crds/cert-manager/\`

**Merge this PR to \`main\`, not #${SOURCE_PR}**, so \`main\` always carries matching pins and rendered manifests.

This PR is updated automatically when new commits are pushed to #${SOURCE_PR}.
EOF

COMPANION_PR=""
if [[ -n "${EXISTING}" ]]; then
  COMPANION_PR="${EXISTING}"
  echo "Open companion PR already exists: #${COMPANION_PR}"
  gh pr edit "${COMPANION_PR}" --repo "${GITHUB_REPOSITORY}" --title "${COMPANION_TITLE}" --body-file "${BODY_FILE}" || true
else
  gh pr create \
    --repo "${GITHUB_REPOSITORY}" \
    --base main \
    --head "${BRANCH}" \
    --title "${COMPANION_TITLE}" \
    --body-file "${BODY_FILE}"
  COMPANION_PR="$(gh pr list --repo "${GITHUB_REPOSITORY}" --head "${BRANCH}" --json number --jq '.[0].number')"
  if [[ -n "${COMPANION_PR}" ]]; then
    gh pr edit "${COMPANION_PR}" --repo "${GITHUB_REPOSITORY}" --add-label "automated,dependencies" 2>/dev/null || true
    echo "Created companion PR #${COMPANION_PR}"
  fi
fi

# Cross-link on the source PR so its timeline shows the companion (body-only
# links on the companion PR do not notify the source thread).
NOTIFY_MARKER="<!-- konflux-manifest-companion-notify:${SOURCE_PR} -->"
if [[ -n "${COMPANION_PR}" ]]; then
  if ! gh pr view "${SOURCE_PR}" --repo "${GITHUB_REPOSITORY}" --json comments --jq -r '(.comments // [])[] | .body // empty' 2>/dev/null | grep -qF "${NOTIFY_MARKER}"; then
    gh pr comment "${SOURCE_PR}" --repo "${GITHUB_REPOSITORY}" --body "${NOTIFY_MARKER}

Manifest companion PR (regenerated \`operator/pkg/manifests\` and third-party Helm outputs for this branch): #${COMPANION_PR}

**Prefer merging #${COMPANION_PR}** to \`main\` instead of this PR." || true
  fi
fi
