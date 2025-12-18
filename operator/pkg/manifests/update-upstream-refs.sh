#!/bin/bash
#
# Update all upstream kustomization references to the latest commit on their default branch
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_DIR="${SCRIPT_DIR}/../../upstream-kustomizations"

# Check if gh CLI is available
if ! command -v gh &> /dev/null; then
    echo "Error: GitHub CLI (gh) is not installed" >&2
    exit 1
fi

# Function to get the latest commit SHA from a GitHub repo's default branch
get_latest_commit() {
    local repo_url="$1"
    local owner_repo

    # Extract owner/repo from URL (e.g., https://github.com/owner/repo/path -> owner/repo)
    # URLs can have paths after the repo name, so we extract just owner/repo
    if [[ "${repo_url}" =~ https://github\.com/([^/]+/[^/?]+) ]]; then
        owner_repo="${BASH_REMATCH[1]}"
    else
        echo "Error: Invalid GitHub URL format: ${repo_url}" >&2
        return 1
    fi

    # Get default branch and latest commit SHA
    local default_branch
    default_branch=$(gh api "repos/${owner_repo}" --jq .default_branch 2>/dev/null || echo "main")

    local latest_commit
    latest_commit=$(gh api "repos/${owner_repo}/commits/${default_branch}" --jq .sha 2>/dev/null)

    if [[ -z "${latest_commit}" ]]; then
        echo "Error: Failed to get latest commit for ${owner_repo}" >&2
        return 1
    fi

    echo "${latest_commit}"
}

# Function to update refs in a kustomization file
update_kustomization_file() {
    local file="$1"
    local repo_url="$2"
    local old_ref="$3"
    local new_ref="$4"

    # Update ?ref= parameter in the URL
    sed -i "s|${repo_url}?ref=${old_ref}|${repo_url}?ref=${new_ref}|g" "${file}"

    # Also update matching newTag: if it matches the old commit SHA
    # This handles cases where image tags use the commit SHA
    sed -i "s|newTag: ${old_ref}|newTag: ${new_ref}|g" "${file}"
}

# Find all kustomization.yaml files
echo "Scanning for kustomization files..."
kustomization_files=$(find "${SOURCE_DIR}" -name "kustomization.yaml" -type f)

if [[ -z "${kustomization_files}" ]]; then
    echo "No kustomization.yaml files found in ${SOURCE_DIR}" >&2
    exit 1
fi

# Extract all unique GitHub repo URLs with their current refs
declare -A repo_refs
declare -A repo_urls

while IFS= read -r file; do
    # Extract GitHub URLs with ?ref= parameters
    while IFS= read -r line; do
        if [[ "${line}" =~ https://github\.com/([^/]+/[^/?]+)([^?]*)\?ref=([a-f0-9]{40}) ]]; then
            repo_path="${BASH_REMATCH[1]}${BASH_REMATCH[2]}"
            repo_url="https://github.com/${repo_path}"
            current_ref="${BASH_REMATCH[3]}"

            # Store the repo URL and current ref
            repo_refs["${repo_url}"]="${current_ref}"
            repo_urls["${repo_url}"]="${repo_url}"
        fi
    done < <(grep -E "https://github\.com/[^?]+\?ref=[a-f0-9]{40}" "${file}" || true)
done <<< "${kustomization_files}"

if [[ ${#repo_urls[@]} -eq 0 ]]; then
    echo "No GitHub repo references found to update"
    exit 0
fi

echo "Found ${#repo_urls[@]} unique upstream repositories to check"
echo ""

# Get latest commits for each unique repo
declare -A latest_refs
updated_count=0

for repo_url in "${!repo_urls[@]}"; do
    current_ref="${repo_refs[${repo_url}]}"
    echo "Checking ${repo_url}..."
    echo "  Current ref: ${current_ref}"

    if ! latest_ref=$(get_latest_commit "${repo_url}") || [[ -z "${latest_ref}" ]]; then
        echo "  ✗ Failed to get latest commit, skipping" >&2
        continue
    fi

    latest_refs["${repo_url}"]="${latest_ref}"
    echo "  Latest ref:  ${latest_ref}"

    if [[ "${current_ref}" == "${latest_ref}" ]]; then
        echo "  ✓ Already up to date"
    else
        echo "  → Will update"
        ((updated_count++)) || true
    fi
    echo ""
done

if [[ ${updated_count} -eq 0 ]]; then
    echo "All upstream references are already up to date!"
    exit 0
fi

echo "Updating ${updated_count} repository reference(s)..."
echo ""

# Update all kustomization files
for repo_url in "${!latest_refs[@]}"; do
    old_ref="${repo_refs[${repo_url}]}"
    new_ref="${latest_refs[${repo_url}]}"

    if [[ "${old_ref}" == "${new_ref}" ]]; then
        continue
    fi

    echo "Updating ${repo_url}: ${old_ref} → ${new_ref}"

    # Update all files that reference this repo
    while IFS= read -r file; do
        if grep -q "${repo_url}?ref=${old_ref}" "${file}" 2>/dev/null || \
           grep -q "newTag: ${old_ref}" "${file}" 2>/dev/null; then
            update_kustomization_file "${file}" "${repo_url}" "${old_ref}" "${new_ref}"
            file_dir=$(dirname "${file}")
            file_name=$(basename "${file}")
            echo "  ✓ Updated $(basename "${file_dir}")/${file_name}"
        fi
    done <<< "${kustomization_files}"
done

echo ""
echo "✓ Successfully updated ${updated_count} upstream reference(s)"

