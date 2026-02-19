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
# Dependencies: gh, jq, git, yq
#
# Example:
#   export GH_TOKEN=$(gh auth token)
#   generate-changelog.sh v0.0.8 v0.0.9
#   generate-changelog.sh v0.0.8 v0.0.9 release-service

if [ "$#" -lt 2 ]; then
  echo "Error: Invalid number of arguments" >&2
  echo "Usage: $0 <old_tag> <new_tag> [component...]" >&2
  exit 1
fi

OLD_TAG="$1"
NEW_TAG="$2"
shift 2

# Component configuration
# Format: "kustomization_path|image_or_pattern|github_org/repo|display_name|extraction_type"
#
# - kustomization_path: Path to the kustomization file containing the image or resource reference
# - image_or_pattern:   For newTag: image name in the `images:` section
#                        For ref: repo pattern to match in `resources:` URLs
# - github_org/repo:    GitHub repository for the Compare API call
# - display_name:       Human-readable name for the changelog section
# - extraction_type:    How to extract the SHA: "newTag" (from images[].newTag) or "ref" (from resources[] ?ref=)
#
# Add new components here to extend changelog coverage.
declare -A COMPONENT_CONFIG=(
  ["release-service"]="operator/upstream-kustomizations/release/core/kustomization.yaml|quay.io/konflux-ci/release-service|konflux-ci/release-service|release-service|newTag"
  ["ui"]="operator/upstream-kustomizations/ui/core/proxy/kustomization.yaml|quay.io/konflux-ci/konflux-ui|konflux-ci/konflux-ui|UI|newTag"
  ["application-api"]="operator/upstream-kustomizations/application-api/kustomization.yaml|redhat-appstudio/application-api|konflux-ci/application-api|Application API|ref"
  ["enterprise-contract"]="operator/upstream-kustomizations/enterprise-contract/core/kustomization.yaml|conforma/crds|conforma/crds|Enterprise Contract|ref"
)

# Bot authors to exclude from changelog
BOT_AUTHORS='["dependabot[bot]", "renovate[bot]", "github-actions[bot]", "konflux-internal-p02[bot]", "red-hat-konflux[bot]"]'

# Determine which components to process.
# Sort keys for deterministic output order (bash associative arrays are unordered).
if [ "$#" -gt 0 ]; then
  COMPONENTS=("$@")
else
  IFS=$'\n' read -r -d '' -a COMPONENTS < <(printf '%s\n' "${!COMPONENT_CONFIG[@]}" | sort && printf '\0') || true
fi

# extract_sha_from_kustomization extracts the newTag for a given image from a
# kustomization file at a given git ref (tag, branch, or HEAD).
#
# This uses the `images:` section (newTag field) rather than `?ref=` URL parameters,
# which allows it to work for components that don't have git resource references
# (e.g., UI only has image references, no ?ref= URLs).
#
# Args: $1=ref, $2=kustomization_path, $3=image_name
extract_sha_from_kustomization() {
  local ref="$1"
  local kustomization_path="$2"
  local image_name="$3"

  local content
  content=$(git show "${ref}:${kustomization_path}" 2>/dev/null) || {
    echo "" # Return empty on failure
    return
  }

  # Find the image entry by name and extract its newTag value using yq.
  # yq outputs the string "null" for missing fields, so filter that out.
  local result
  result=$(echo "$content" | yq eval '.images[] | select(.name == "'"${image_name}"'") | .newTag' - 2>/dev/null) || true
  if [ "$result" = "null" ] || [ -z "$result" ]; then
    echo ""
  else
    echo "$result"
  fi
}

# extract_sha_from_ref_url extracts the ?ref= SHA from a resources URL in a
# kustomization file at a given git ref. Used for components that reference
# upstream via resource URLs rather than image tags (e.g., application-api,
# enterprise-contract).
#
# Args: $1=ref, $2=kustomization_path, $3=repo_pattern
extract_sha_from_ref_url() {
  local ref="$1"
  local kustomization_path="$2"
  local repo_pattern="$3"

  local content
  content=$(git show "${ref}:${kustomization_path}" 2>/dev/null) || {
    echo ""
    return
  }

  # Use yq to find the matching resource URL, then extract the ?ref= value.
  # sed -n with /p only prints lines where ?ref= was found, returning empty
  # if the URL has no ref parameter.
  echo "$content" \
    | yq eval '.resources[] | select(contains("'"${repo_pattern}"'"))' - 2>/dev/null \
    | sed -n 's/.*?ref=//p' \
    | head -n 1 || true
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

  local kustomization_path image_or_pattern github_repo display_name extraction_type
  IFS='|' read -r kustomization_path image_or_pattern github_repo display_name extraction_type <<< "$config"

  echo "Processing component: ${display_name} (${github_repo})" >&2

  # Extract SHAs at each tag using the appropriate extraction method
  local old_sha new_sha
  case "$extraction_type" in
    newTag)
      old_sha=$(extract_sha_from_kustomization "$OLD_TAG" "$kustomization_path" "$image_or_pattern")
      new_sha=$(extract_sha_from_kustomization "$NEW_TAG" "$kustomization_path" "$image_or_pattern")
      ;;
    ref)
      old_sha=$(extract_sha_from_ref_url "$OLD_TAG" "$kustomization_path" "$image_or_pattern")
      new_sha=$(extract_sha_from_ref_url "$NEW_TAG" "$kustomization_path" "$image_or_pattern")
      ;;
    *)
      echo "  Error: Unknown extraction_type '${extraction_type}' for ${display_name}" >&2
      return 1
      ;;
  esac

  # Handle new/removed component edge cases before treating as extraction errors
  if [ -z "$old_sha" ] && [ -n "$new_sha" ]; then
    echo "  New component detected: ${display_name}" >&2
    echo "### ${display_name}"
    echo "- *new*: component added in this release"
    echo ""
    return 0
  fi

  if [ -n "$old_sha" ] && [ -z "$new_sha" ]; then
    echo "  Removed component detected: ${display_name}" >&2
    echo "### ${display_name}"
    echo "- *removed*: component removed in this release"
    echo ""
    return 0
  fi

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
