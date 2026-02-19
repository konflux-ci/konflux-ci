#!/bin/bash
set -euo pipefail

# Verify Releases Script
# 1) When there were commits on the current branch in the past 7 days, at least one
#    matching release (prerelease or stable) must exist in that period.
# 2) Every stable-format tag (vX.Y.Z, no -rc) created in the past 7 days must have
#    a GitHub release (no dangling stable tags).
#
# Usage:
#   verify-releases.sh <repository> [tag_prefix]
#
# Arguments:
#   repository   - Repository in format owner/repo (required)
#   tag_prefix   - Optional: prefix for tags including trailing dot (e.g. v1.2. → matches v1.2.*).
#                  Used to scope both checks. Empty = any tag.
#
# The script must be run from a repo checkout (workflow checks out the branch; fetch-depth: 0).
#
# Environment Variables:
#   GH_TOKEN      - GitHub token for API access (required)
#   GITHUB_ENV    - Path to GitHub Actions environment file (automatically set by GitHub Actions)
#
# The script sets the following for use by the workflow (when GITHUB_ENV/GITHUB_OUTPUT are set):
#   GITHUB_ENV:   COMMIT_COUNT, VERIFICATION_FAILED
#   GITHUB_OUTPUT: verification_failed (use steps.<id>.outputs.verification_failed in if:)
#
# Exit Codes:
#   0 - Success (matching release exists OR no commits)
#   1 - Unexpected failure: Script error
#   2 - Expected failure: No matching release but commits exist (should create verification issue)
#
# Example:
#   export GH_TOKEN="your_token"
#   verify-releases.sh owner/repo
#   verify-releases.sh owner/repo v1.2.

if [ $# -lt 1 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <repository> [tag_prefix]"
  echo "  tag_prefix: optional, e.g. v1.2. to scope to v1.2.*"
  exit 1
fi

REPOSITORY="$1"
TAG_PREFIX="${2:-}"

# Verify GH_TOKEN is set
if [ -z "${GH_TOKEN:-}" ]; then
  echo "Error: GH_TOKEN environment variable is not set"
  exit 1
fi

export GH_TOKEN

# Initialize VERIFICATION_FAILED to false (env for later steps; output for workflow if:)
echo "VERIFICATION_FAILED=false" >> "${GITHUB_ENV}"
[ -n "${GITHUB_OUTPUT:-}" ] && echo "verification_failed=false" >> "${GITHUB_OUTPUT}"

[ -n "${TAG_PREFIX}" ] && echo "Tag prefix: ${TAG_PREFIX}"
echo "Checking for releases in the past 7 days..."

# Calculate the start time ONCE (7 days ago in seconds)
# This freezes the window so both commands (to get releases and commits) use the exact same instant
START_TIMESTAMP=$(date -u --date='7 days ago' +%s)

# Fetch releases with tagName and isPrerelease for filtering
RELEASES_JSON=$(gh release list --repo "$REPOSITORY" --limit 100 --json publishedAt,tagName 2>&1) || {
  echo "Error: Failed to fetch releases - ${RELEASES_JSON}"
  exit 1
}

# Filter: in window and tag prefix (empty => any tag). Prerelease or stable both count.
RELEASE_COUNT=$(echo "$RELEASES_JSON" | jq --arg start "$START_TIMESTAMP" --arg prefix "$TAG_PREFIX" \
  'map(select(
    (.publishedAt | fromdate) > ($start | tonumber) and
    (.tagName | startswith($prefix))
  )) | length') || {
  echo "Error: Failed to process releases data"
  exit 1
}
if [ -n "${TAG_PREFIX}" ]; then
  echo "Looking for releases with tag ${TAG_PREFIX}* in the past 7 days"
else
  echo "Looking for releases in the past 7 days"
fi

if [ "$RELEASE_COUNT" -gt 0 ]; then
  echo "Found $RELEASE_COUNT matching release(s) in the past 7 days"
  RELEASE_EXISTS="true"
else
  echo "No matching releases found in the past 7 days"
  RELEASE_EXISTS="false"
fi

# Commits on current branch (workflow checks out the branch)
COMMITS=$(git log --since="@$START_TIMESTAMP" \
  --oneline --no-merges HEAD 2>/dev/null || echo "")

COMMIT_COUNT=$(echo "$COMMITS" | wc -l | tr -d ' ')

if [ "$COMMIT_COUNT" -gt 0 ]; then
  echo ""
  echo "Found $COMMIT_COUNT commit(s) in the past 7 days on current branch"
  COMMITS_EXIST="true"
  echo "$COMMITS" > /tmp/recent-commits.txt
  echo ""
  echo "Recent commits:"
  echo "$COMMITS"
else
  echo ""
  echo "No commits found in the past 7 days on current branch"
  COMMITS_EXIST="false"
  COMMIT_COUNT=0
fi

echo "COMMIT_COUNT=${COMMIT_COUNT}" >> "${GITHUB_ENV}"

# --- Check 2: every stable-format tag (vX.Y.Z, no -rc) in the window must have a GitHub release ---
# Same semantic version pattern as auto-tag-main.sh
STABLE_TAG_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+$'
TAGS_WITHOUT_RELEASE=""
while IFS= read -r line; do
  [ -z "$line" ] && continue
  tag="${line%% *}"
  cread="${line##* }"
  if [ "$cread" -lt "$START_TIMESTAMP" ] 2>/dev/null; then
    continue
  fi
  if [[ ! "$tag" =~ $STABLE_TAG_PATTERN ]]; then
    continue
  fi
  if [ -n "${TAG_PREFIX}" ] && [[ "$tag" != "${TAG_PREFIX}"* ]]; then
    continue
  fi
  if ! gh release view "$tag" --repo "$REPOSITORY" >/dev/null 2>&1; then
    TAGS_WITHOUT_RELEASE="${TAGS_WITHOUT_RELEASE}${TAGS_WITHOUT_RELEASE:+$'\n'}$tag"
  fi
done < <(git for-each-ref --format='%(refname:short) %(creatordate:unix)' refs/tags 2>/dev/null || true)

FAILURE_REASONS=""
if [ "$RELEASE_EXISTS" = "false" ] && [ "$COMMITS_EXIST" = "true" ]; then
  FAILURE_REASONS="No matching release in the past 7 days, but commits exist (${COMMIT_COUNT} commit(s))."
fi
if [ -n "$TAGS_WITHOUT_RELEASE" ]; then
  [ -n "$FAILURE_REASONS" ] && FAILURE_REASONS="${FAILURE_REASONS}"$'\n\n'
  FAILURE_REASONS="${FAILURE_REASONS}Stable-format tag(s) in the past 7 days with no GitHub release:"
fi

if [ -n "$FAILURE_REASONS" ]; then
  echo ""
  echo "❌ Verification failed:"
  echo "$FAILURE_REASONS"
  [ -n "$TAGS_WITHOUT_RELEASE" ] && echo "$TAGS_WITHOUT_RELEASE"
  FAILURE_DETAILS=$(mktemp)
  if [ "$RELEASE_EXISTS" = "false" ] && [ "$COMMITS_EXIST" = "true" ] && [ -f /tmp/recent-commits.txt ]; then
    echo "Recent commits (no release in window):"
    echo ""
    cat /tmp/recent-commits.txt >> "$FAILURE_DETAILS"
  fi
  if [ -n "$TAGS_WITHOUT_RELEASE" ]; then
    [ -s "$FAILURE_DETAILS" ] && echo "" >> "$FAILURE_DETAILS"
    echo "Stable tags in window without a GitHub release:"
    echo "$TAGS_WITHOUT_RELEASE" >> "$FAILURE_DETAILS"
  fi
  cat "$FAILURE_DETAILS" > /tmp/recent-commits.txt
  rm -f "$FAILURE_DETAILS"
  echo "VERIFICATION_FAILED=true" >> "${GITHUB_ENV}"
  [ -n "${GITHUB_OUTPUT:-}" ] && echo "verification_failed=true" >> "${GITHUB_OUTPUT}"
  exit 2
fi
