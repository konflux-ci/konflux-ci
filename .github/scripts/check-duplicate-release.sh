#!/bin/bash
set -euo pipefail

# Check Duplicate Release Script
# This script checks if a release already exists for the given image tag and git ref.
# It also detects unreleased commits and can create a GitHub issue if needed.
#
# Usage:
#   check-duplicate-release.sh <image_tag> <git_ref> [create_issue]
#
# Arguments:
#   image_tag   - Image tag to check (e.g., release-sha-abc1234)
#   git_ref     - Git ref to check (commit SHA, branch, or tag)
#   create_issue - Optional: "true" to create GitHub issue for unreleased commits, "false" otherwise (default: false)
#
# Outputs (to stdout, key=value format):
#   should_skip=true|false
#   latest_release_image_tag=<tag> or empty
#   latest_release_git_ref=<ref> or empty
#   has_unreleased_commits=true|false
#   issue_created=true|false or empty
#
# Exit codes:
#   0 - Success
#   1 - Error occurred

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
    echo "Error: Invalid number of arguments" >&2
    echo "Usage: $0 <image_tag> <git_ref> [create_issue]" >&2
    exit 1
fi

IMAGE_TAG="$1"
GIT_REF="$2"
CREATE_ISSUE="${3:-false}"

echo "Checking for duplicate releases..." >&2
echo "Image tag: ${IMAGE_TAG}" >&2
echo "Git ref: ${GIT_REF}" >&2

# Get latest non-draft release tag name first
latest_tag=$(gh release list --exclude-drafts --limit 1 --json tagName --jq '.[0].tagName' 2>/dev/null || echo "")

if [ -z "${latest_tag}" ] || [ "${latest_tag}" == "null" ]; then
    echo "No previous releases found" >&2
    echo "should_skip=false"
    echo "latest_release_image_tag="
    echo "latest_release_git_ref="
    echo "has_unreleased_commits=false"
    echo "issue_created="
    exit 0
fi

echo "Latest release tag: ${latest_tag}" >&2

# Get body from latest release using gh release view
latest_body=$(gh release view "${latest_tag}" --json body --jq '.body // ""' 2>/dev/null || echo "")

# Extract image tag and git ref from release notes
# Look for lines like:
# - **Tag**: release-sha-abc1234
# - **Git Ref**: c515b60f474cb00a11176e5b400205a679b68aac
latest_release_image_tag=$(echo "${latest_body}" | grep -E '^[[:space:]]*-\s+\*\*Tag\*\*:' | sed -E 's/^[[:space:]]*-\s+\*\*Tag\*\*:[[:space:]]*(.*)/\1/' | head -1 || echo "")
latest_release_git_ref=$(echo "${latest_body}" | grep -E '^[[:space:]]*-\s+\*\*Git Ref\*\*:' | sed -E 's/^[[:space:]]*-\s+\*\*Git Ref\*\*:[[:space:]]*(.*)/\1/' | head -1 || echo "")

echo "Latest release image tag: ${latest_release_image_tag:-<not found>}" >&2
echo "Latest release git ref: ${latest_release_git_ref:-<not found>}" >&2

# Check if this is a duplicate (same image tag)
should_skip="false"
if [ -n "${latest_release_image_tag}" ] && [ "${latest_release_image_tag}" == "${IMAGE_TAG}" ]; then
    should_skip="true"
    echo "⚠️  Duplicate detected: Same image tag already released (${IMAGE_TAG})" >&2
fi

# If no duplicate, we can proceed with release
if [ "${should_skip}" != "true" ]; then
    echo "✅ No duplicate detected, proceeding with release" >&2
    echo "should_skip=false"
    echo "latest_release_image_tag=${latest_release_image_tag}"
    echo "latest_release_git_ref=${latest_release_git_ref}"
    echo "has_unreleased_commits=false"
    echo "issue_created="
    exit 0
fi

# If we're skipping due to duplicate, check if there are unreleased commits
has_unreleased_commits="false"
issue_created=""

# Check if the git ref we tried to release is not the latest on main
current_ref_sha=$(git rev-parse "${GIT_REF}" 2>/dev/null || echo "")
latest_main_sha=$(git rev-parse "origin/main" 2>/dev/null || echo "")

if [ "${current_ref_sha}" != "${latest_main_sha}" ]; then
    has_unreleased_commits="true"
    echo "⚠️  Unreleased commits detected: git ref ${GIT_REF} is not the latest on main" >&2

    # Create GitHub issue if requested
    if [ "${CREATE_ISSUE}" == "true" ]; then
        issue_title="⚠️ Unreleased commits detected but no new image available"
        issue_body="A duplicate release was detected (same image tag already released), but the git ref being released is not the latest on main.

**Git ref being released:** ${GIT_REF} (${current_ref_sha})
**Latest on main:** ${latest_main_sha}
**Image tag:** ${IMAGE_TAG}

This may indicate a build failure or that the image build pipeline needs attention.

---
*This issue was automatically created by the release workflow*"

        # Check if an open issue with this exact title already exists
        existing_issue=$(gh issue list --label "automated" --label "release" --state open --json number,title --jq '.[] | select(.title == "'"${issue_title}"'") | .number' 2>/dev/null | head -1 || echo "")

        if [ -z "${existing_issue}" ]; then
            # Create a new issue
            created_issue=$(gh issue create \
                --title "${issue_title}" \
                --body "${issue_body}" \
                --label "automated" \
                --label "release" \
                --json number --jq '.number' 2>/dev/null || echo "")

            if [ -n "${created_issue}" ]; then
                issue_created="true"
                echo "✅ Created GitHub issue #${created_issue}" >&2
            else
                echo "⚠️  Failed to create GitHub issue" >&2
                issue_created="false"
            fi
        else
            # Add a comment to the existing issue
            comment_body="Duplicate release detected again on $(date -u +"%Y-%m-%d %H:%M:%S UTC")

**Git ref being released:** ${GIT_REF} (${current_ref_sha})
**Latest on main:** ${latest_main_sha}
**Image tag:** ${IMAGE_TAG}

This may indicate a build failure or that the image build pipeline needs attention."

            if gh issue comment "${existing_issue}" --body "${comment_body}" >/dev/null 2>&1; then
                issue_created="true"
                echo "✅ Added comment to existing GitHub issue #${existing_issue}" >&2
            else
                echo "⚠️  Failed to add comment to issue #${existing_issue}" >&2
                issue_created="false"
            fi
        fi
    fi
fi

# Output results
echo "should_skip=${should_skip}"
echo "latest_release_image_tag=${latest_release_image_tag}"
echo "latest_release_git_ref=${latest_release_git_ref}"
echo "has_unreleased_commits=${has_unreleased_commits}"
if [ -n "${issue_created}" ]; then
    echo "issue_created=${issue_created}"
else
    echo "issue_created="
fi

if [ "${should_skip}" == "true" ]; then
    echo "" >&2
    echo "⏭️  Skipping release creation (duplicate detected)" >&2
    exit 0
fi

echo "✅ No duplicate detected, proceeding with release" >&2
exit 0
