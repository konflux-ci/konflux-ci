#!/bin/bash
set -euo pipefail

# Verify Releases Script
# For the current branch (already checked out by the workflow):
#   1. Determine the stream (X.Y): from the branch name for release-x.y branches,
#      or from the latest version tag for main.
#   2. Find the latest tag for this stream (vX.Y.Z or vX.Y.Z-rc.W).
#   3. Verify every stable tag (vX.Y.Z) for this stream created in the past 7 days
#      has a corresponding GitHub release.
#   4. If the latest tag is an RC and its stable counterpart is not yet tagged, verify it has a GitHub release.
#
# Usage:
#   verify-releases.sh <repository> <branch>
#
# Arguments:
#   repository - Repository in format owner/repo (required)
#   branch     - Branch name, e.g. main or release-1.2 (required)
#
# Must be run from a repo checkout with the target branch checked out
# (fetch-depth: 0, fetch-tags: true).
#
# Environment Variables:
#   GH_TOKEN      - GitHub token for API access (required)
#   GITHUB_OUTPUT - Path to GitHub Actions step output file (set automatically)
#
# The script sets:
#   GITHUB_OUTPUT: verification_failed (true/false)
#
# Exit Codes:
#   0 - All checks passed (or no tags to verify)
#   1 - Unexpected failure (script error)
#   2 - Verification failure (missing releases)
#
# Example:
#   export GH_TOKEN="your_token"
#   verify-releases.sh owner/repo release-1.2

if [ $# -lt 2 ]; then
  echo "Error: Invalid number of arguments"
  echo "Usage: $0 <repository> <branch>"
  exit 1
fi

REPOSITORY="$1"
BRANCH="$2"

if [ -z "${GH_TOKEN:-}" ]; then
  echo "Error: GH_TOKEN environment variable is not set"
  exit 1
fi

export GH_TOKEN

set_output() {
  [ -n "${GITHUB_OUTPUT:-}" ] && echo "$1=$2" >> "${GITHUB_OUTPUT}"
}

set_output "verification_failed" "false"

STABLE_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+$'
VERSION_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$'

# Determine the version stream (X.Y) to scope all tag lookups.
# For release-x.y branches the stream is encoded in the branch name.
# For main we derive it from the highest version tag reachable from HEAD.
if [[ "$BRANCH" =~ ^release-([0-9]+\.[0-9]+)$ ]]; then
  STREAM="${BASH_REMATCH[1]}"
  echo "Stream from branch name: $STREAM"
elif [ "$BRANCH" = "main" ]; then
  HIGHEST=$(git tag --merged=HEAD 2>/dev/null \
    | grep -E "$VERSION_PATTERN" | sort -V | tail -1 || true)
  if [ -z "$HIGHEST" ]; then
    echo "Error: No version tags reachable from HEAD on main."
    exit 1
  fi
  [[ "$HIGHEST" =~ ^v([0-9]+\.[0-9]+)\. ]]
  STREAM="${BASH_REMATCH[1]}"
  echo "Stream from latest tag ($HIGHEST): $STREAM"
else
  echo "Error: Unexpected branch format: $BRANCH (expected main or release-x.y)"
  exit 1
fi

TAG_PREFIX="v${STREAM}."

# All version tags for this stream reachable from HEAD.
# Captured once and reused for both LATEST and the Check 2 stable-counterpart lookup,
# so both checks share the same --merged=HEAD reachability scope.
STREAM_TAGS=$(git tag --merged=HEAD 2>/dev/null \
  | grep -E "$VERSION_PATTERN" | grep -F "${TAG_PREFIX}" || true)

# Latest tag for this stream.
# Note: sort -V ranks RC suffixes after the bare version (v0.0.12 < v0.0.12-rc.0).
# When both an RC and its stable counterpart exist for the same version (e.g. v0.0.12
# and v0.0.12-rc.0), LATEST will be the RC tag.
LATEST=$(echo "$STREAM_TAGS" | sort -V | tail -1 || true)

if [ -z "$LATEST" ]; then
  echo "Error: No version tags for stream ${STREAM} reachable from HEAD."
  exit 1
fi

echo "Latest tag for stream ${STREAM}: $LATEST"

# Limit Check 1 to tags created within the past 7 days to avoid re-checking
# historical releases on every run.
START_TIMESTAMP=$(date -u --date='7 days ago' +%s)
echo "Verification window: past 7 days (since $(date -u --date="@$START_TIMESTAMP" +%Y-%m-%dT%H:%M:%SZ))"

FAILURE_REASONS=""

# Check 1: every stable tag for this stream created in the verification window
# must have a corresponding GitHub release.
echo ""
echo "=== Checking stable tags for stream ${STREAM} in verification period ==="
TAGS_WITHOUT_RELEASE=""
STABLE_CHECKED=0
while IFS= read -r line; do
  [ -z "$line" ] && continue
  tag="${line%% *}"
  cdate="${line##* }"
  # Skip tags outside the 7-day window.
  [ "$cdate" -lt "$START_TIMESTAMP" ] 2>/dev/null && continue
  # Skip RC tags — only stable releases are checked here.
  [[ ! "$tag" =~ $STABLE_PATTERN ]] && continue
  # Skip tags that belong to a different stream.
  [[ "$tag" != "${TAG_PREFIX}"* ]] && continue
  STABLE_CHECKED=$((STABLE_CHECKED + 1))
  echo "  Checking stable tag: $tag"
  if ! gh release view "$tag" --repo "$REPOSITORY" >/dev/null 2>&1; then
    TAGS_WITHOUT_RELEASE="${TAGS_WITHOUT_RELEASE}${TAGS_WITHOUT_RELEASE:+$'\n'}  - $tag"
    echo "    ❌ No GitHub release found"
  else
    echo "    ✅ GitHub release exists"
  fi
done < <(git for-each-ref --format='%(refname:short) %(creatordate:unix)' refs/tags 2>/dev/null || true)

if [ "$STABLE_CHECKED" -eq 0 ]; then
  echo "  No stable tags for stream ${STREAM} in the verification period."
fi

if [ -n "$TAGS_WITHOUT_RELEASE" ]; then
  FAILURE_REASONS="Stable tag(s) for stream ${STREAM} in the past 7 days without a GitHub release:
${TAGS_WITHOUT_RELEASE}"
fi

# Check 2: if the latest tag is an RC, verify its stable counterpart has been tagged.
# - If the stable counterpart exists and is reachable from HEAD (e.g. v0.0.12 for
#   v0.0.12-rc.0), the RC was promoted and Check 1 already covers the stable release — skip.
# - If the stable counterpart is absent or not reachable from HEAD, the RC is still
#   active and must have a GitHub prerelease.
if [[ ! "$LATEST" =~ $STABLE_PATTERN ]]; then
  # Strip the -rc.N suffix to get the stable version this RC targets.
  STABLE_FOR_RC="${LATEST%-rc.*}"
  # Use STREAM_TAGS (already scoped to --merged=HEAD) so we only treat the RC as
  # promoted when the stable tag is reachable from the current branch HEAD.
  if echo "$STREAM_TAGS" | grep -qxF "$STABLE_FOR_RC"; then
    echo ""
    echo "=== Latest RC ${LATEST} already promoted to stable (${STABLE_FOR_RC}) — skipping RC check ==="
  else
    echo ""
    echo "=== Latest tag is RC: checking for prerelease ==="
    echo "  Checking RC tag: $LATEST"
    if ! gh release view "$LATEST" --repo "$REPOSITORY" >/dev/null 2>&1; then
      [ -n "$FAILURE_REASONS" ] && FAILURE_REASONS="${FAILURE_REASONS}"$'\n\n'
      FAILURE_REASONS="${FAILURE_REASONS}Latest RC tag ${LATEST} has no GitHub release."
      echo "    ❌ No GitHub release found"
    else
      echo "    ✅ GitHub release exists"
    fi
  fi
fi

if [ -n "$FAILURE_REASONS" ]; then
  echo ""
  echo "❌ Verification failed:"
  echo "$FAILURE_REASONS"
  echo "$FAILURE_REASONS" > /tmp/verification-details.txt
  set_output "verification_failed" "true"
  exit 2
fi

echo ""
echo "✅ All verification checks passed for stream ${STREAM}"
