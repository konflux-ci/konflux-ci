#!/bin/bash
set -euo pipefail

# List one draft Community Operator PR by the current user and output its release tag.
# Used by the "mark one ready" workflow to pick a draft PR to update and mark ready.
#
# Output (stdout, for GITHUB_OUTPUT):
#   count=0  when no draft PR or release tag could not be parsed
#   count=1
#   tag=<release_tag>
#
# Exits 1 (workflow failure → issue created) when there is already an open non-draft
# PR from the bot; we only allow one ready PR at a time to avoid catalog-update conflicts.
#
# Environment:
#   GH_TOKEN or GITHUB_TOKEN - used for gh (same identity that created the PRs)
#   GITHUB_OUTPUT (optional)  - if set, write key=value lines here; else print to stdout

UPSTREAM_REPO="${UPSTREAM_REPO:-redhat-openshift-ecosystem/community-operators-prod}"

write_output() {
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "$1" >> "$GITHUB_OUTPUT"
  else
    echo "$1"
  fi
}

log() { echo "$*" >&2; }

if [ -z "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]; then
  log "Error: GH_TOKEN or GITHUB_TOKEN is required"
  exit 1
fi

PR_JSON=$(GH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN}}" gh pr list --repo "${UPSTREAM_REPO}" \
  --author "@me" --state open --search "is:draft" --json number,body --limit 1)

if [ -z "$PR_JSON" ] || [ "$PR_JSON" = "[]" ]; then
  log "No draft PRs found. Skipping."
  write_output "count=0"
  exit 0
fi

PR_NUMBER=$(echo "$PR_JSON" | jq -r '.[0].number')
BODY=$(echo "$PR_JSON" | jq -r '.[0].body')
RELEASE_TAG=$(echo "$BODY" | grep -oE 'releases/tag/[^ )]+' | head -1 | sed 's|releases/tag/||' || true)

if [ -z "$RELEASE_TAG" ]; then
  log "Could not parse release tag from PR #${PR_NUMBER} body. Skipping."
  write_output "count=0"
  exit 0
fi

# Only one non-draft (ready) PR at a time to avoid catalog-update PR conflicts.
OPEN_READY=$(GH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN}}" gh pr list --repo "${UPSTREAM_REPO}" \
  --author "@me" --state open --json number,isDraft -q '[.[] | select(.isDraft == false)] | length')
if [ "${OPEN_READY:-0}" -gt 0 ]; then
  log "A non-draft PR from this bot is already open. Not marking another draft ready."
  log "Wait for it to be merged or closed before the next run can promote a draft."
  exit 1
fi

log "Found draft PR #${PR_NUMBER} (release ${RELEASE_TAG}). Will update and mark ready."
write_output "count=1"
write_output "tag=${RELEASE_TAG}"
