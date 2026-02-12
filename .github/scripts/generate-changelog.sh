#!/bin/bash
set -euo pipefail

# Generate Changelog Script
# Generates a markdown changelog from upstream conventional commits between two operator tags.
#
# For each configured component, this script:
# 1. Extracts the upstream commit SHA from the kustomization file at each tag
# 2. Queries the GitHub API for commits between those SHAs
# 3. Filters to conventional commit types (feat, fix) excluding bot authors
# 4. Outputs formatted markdown to stdout
#
# Usage:
#   generate-changelog.sh <old_tag> <new_tag> [component...]
#
# Arguments:
#   old_tag       - Previous release tag (e.g., v0.0.8)
#   new_tag       - Current release tag (e.g., v0.0.9)
#   component...  - Optional list of components to process (default: all configured)
#
# Environment:
#   GH_TOKEN      - GitHub token for API access (required)
#
# Dependencies: gh, jq, git
#
# Example:
#   export GH_TOKEN=$(gh auth token)
#   generate-changelog.sh v0.0.8 v0.0.9
#   generate-changelog.sh v0.0.8 v0.0.9 release-service

if [ $# -lt 2 ]; then
  echo "Error: Invalid number of arguments" >&2
  echo "Usage: $0 <old_tag> <new_tag> [component...]" >&2
  exit 1
fi

OLD_TAG="$1"
NEW_TAG="$2"
shift 2

# Component configuration
# Format: "kustomization_path|github_org/repo|display_name"
# Add new components here to extend changelog coverage.
declare -A COMPONENT_CONFIG=(
  ["release-service"]="operator/upstream-kustomizations/release/core/kustomization.yaml|konflux-ci/release-service|release-service"
)

# Bot authors to exclude from changelog
BOT_AUTHORS='["dependabot[bot]", "renovate[bot]", "github-actions[bot]", "konflux-internal-p02[bot]", "red-hat-konflux[bot]"]'

# Determine which components to process
if [ $# -gt 0 ]; then
  COMPONENTS=("$@")
else
  COMPONENTS=("${!COMPONENT_CONFIG[@]}")
fi

# extract_sha_from_kustomization extracts the ?ref=<sha> from a kustomization file
# at a given git tag.
# Args: $1=tag, $2=kustomization_path, $3=github_repo (org/name)
extract_sha_from_kustomization() {
  local tag="$1"
  local kustomization_path="$2"
  local github_repo="$3"

  local content
  content=$(git show "${tag}:${kustomization_path}" 2>/dev/null) || {
    echo "" # Return empty on failure
    return
  }

  # Extract the ref= parameter from the GitHub URL for this component
  echo "$content" | grep -oP "github\.com/${github_repo}[^?]*\?ref=\K[a-f0-9]+" | head -1 || true
}

# format_commit formats a single commit as a markdown list item.
# Args: $1=commit_message
format_commit() {
  local message="$1"

  # Extract conventional commit type and description
  # Matches: type(scope): description or type: description
  local type description
  local pattern='^([a-z]+)(\([^)]*\))?:[[:space:]]*(.+)'
  if [[ "$message" =~ $pattern ]]; then
    type="${BASH_REMATCH[1]}"
    description="${BASH_REMATCH[3]}"
    echo "- *${type}*: ${description}"
  fi
}

# generate_component_changelog generates the changelog section for a single component.
# Args: $1=component_name
# Returns: 0 if changes were found, 1 otherwise
generate_component_changelog() {
  local component="$1"
  local config="${COMPONENT_CONFIG[$component]}"

  local kustomization_path github_repo display_name
  IFS='|' read -r kustomization_path github_repo display_name <<< "$config"

  echo "Processing component: ${display_name} (${github_repo})" >&2

  # Extract SHAs at each tag
  local old_sha new_sha
  old_sha=$(extract_sha_from_kustomization "$OLD_TAG" "$kustomization_path" "$github_repo")
  new_sha=$(extract_sha_from_kustomization "$NEW_TAG" "$kustomization_path" "$github_repo")

  if [ -z "$old_sha" ]; then
    echo "  Warning: Could not extract SHA from ${kustomization_path} at tag ${OLD_TAG}" >&2
    return 1
  fi

  if [ -z "$new_sha" ]; then
    echo "  Warning: Could not extract SHA from ${kustomization_path} at tag ${NEW_TAG}" >&2
    return 1
  fi

  if [ "$old_sha" = "$new_sha" ]; then
    echo "  No changes (same SHA: ${old_sha:0:12})" >&2
    return 1
  fi

  echo "  Comparing ${old_sha:0:12}..${new_sha:0:12}" >&2

  # Query GitHub API for commits between the two SHAs
  local api_response
  api_response=$(gh api "repos/${github_repo}/compare/${old_sha}...${new_sha}" \
    --header "Accept: application/vnd.github+json" 2>/dev/null) || {
    echo "  Warning: GitHub API call failed for ${github_repo}" >&2
    return 1
  }

  # Warn if the response is truncated (GitHub compare API caps at 250 commits)
  local total_commits returned_commits
  total_commits=$(echo "$api_response" | jq '.total_commits // 0') || true
  returned_commits=$(echo "$api_response" | jq '.commits | length') || true
  if [ "$total_commits" -gt "$returned_commits" ] 2>/dev/null; then
    echo "  Warning: ${total_commits} total commits but only ${returned_commits} returned (API limit)" >&2
  fi

  # Filter commits: keep feat/fix, exclude bots
  local filtered_commits
  filtered_commits=$(echo "$api_response" | jq -r --argjson bots "$BOT_AUTHORS" '
    .commits[]
    | select(
        ((.author.login // "") as $login |
          ($bots | map(. == $login) | any | not))
        and
        (.commit.message | split("\n")[0] | test("^(feat|fix)(\\(.*\\))?:"))
      )
    | .commit.message | split("\n")[0]
  ' 2>/dev/null) || {
    echo "  Warning: Failed to filter commits for ${github_repo}" >&2
    return 1
  }

  if [ -z "$filtered_commits" ]; then
    echo "  No conventional commits (feat/fix) found" >&2
    return 1
  fi

  # Output markdown section for this component
  echo "### ${display_name}"
  while IFS= read -r message; do
    format_commit "$message"
  done <<< "$filtered_commits"
  echo ""

  local count
  count=$(echo "$filtered_commits" | wc -l | tr -d ' ')
  echo "  Found ${count} conventional commit(s)" >&2
  return 0
}

# Main: generate changelog
has_changes=false

for component in "${COMPONENTS[@]}"; do
  if [ -z "${COMPONENT_CONFIG[$component]+x}" ]; then
    echo "Warning: Unknown component '${component}', skipping" >&2
    continue
  fi

  section=$(generate_component_changelog "$component") && {
    if [ "$has_changes" = false ]; then
      echo "## Upstream Changes"
      echo ""
      has_changes=true
    fi
    echo "$section"
  }
done

if [ "$has_changes" = false ]; then
  echo "No upstream conventional commits found between ${OLD_TAG} and ${NEW_TAG}" >&2
fi
