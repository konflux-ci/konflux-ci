#!/bin/bash
#
# Process a single component: update upstream refs, build kustomization, and create PR if needed
#
# Usage: process-component.sh <component-name> <workspace-root>
#
# Outputs result in format: "component-name:status:message"
# Status can be: success, failed, up-to-date, no-changes
# Exit codes: 0 = success/up-to-date/no-changes, 1 = failed

set -euo pipefail

COMPONENT="${1:-}"
WORKSPACE_ROOT="${2:-}"

if [[ -z "${COMPONENT}" ]]; then
    echo "Error: Component name is required" >&2
    echo "Usage: $0 <component-name> <workspace-root>" >&2
    exit 1
fi

if [[ -z "${WORKSPACE_ROOT}" ]]; then
    echo "Error: Workspace root is required" >&2
    echo "Usage: $0 <component-name> <workspace-root>" >&2
    exit 1
fi

# Resolve workspace root to absolute path
WORKSPACE_ROOT="$(cd "${WORKSPACE_ROOT}" && pwd)"

# Detect if running in GitHub Actions
LOCAL_MODE=false
if [[ "${GITHUB_ACTIONS:-}" != "true" ]]; then
    LOCAL_MODE=true
    echo "Running in local mode (skipping git and PR operations)"
fi

# Change to workspace root for git operations
cd "${WORKSPACE_ROOT}"

# Ensure we're on main branch and clean (only in CI)
if [[ "${LOCAL_MODE}" != "true" ]]; then
    git checkout main
    git reset --hard origin/main
    git clean -fd
fi

# Step 1: Update upstream references for this component (if it has any)
echo "Step 1: Checking for upstream ref updates for ${COMPONENT}..."
cd operator/pkg/manifests
set +e
# Only update refs that affect this component
COMPONENT_FILTER="${COMPONENT}" bash update-upstream-refs.sh
ref_update_exit=$?
set -e
cd "${WORKSPACE_ROOT}"

# If component has no upstream refs, that's okay - continue to build step
if [ ${ref_update_exit} -ne 0 ] && [ ${ref_update_exit} -ne 1 ]; then
    echo "  ⚠ Ref update had issues, but continuing..."
fi

# Step 2: Build this component
echo "Step 2: Building ${COMPONENT}..."
cd operator/pkg/manifests
source_subdir="../../upstream-kustomizations/${COMPONENT}"
output_subdir="${COMPONENT}"

if [[ ! -d "${source_subdir}" ]]; then
    echo "  ✗ Source directory ${source_subdir} does not exist, skipping" >&2
    echo "${COMPONENT}:failed:source directory not found"
    exit 1
fi

mkdir -p "${output_subdir}"

set +e
if kustomize build "${source_subdir}" > "${output_subdir}/manifests.yaml" 2>&1; then
    echo "  ✓ Successfully built ${COMPONENT}"
else
    echo "  ✗ Failed to build ${COMPONENT}" >&2
    echo "${COMPONENT}:failed:kustomize build error"
    exit 1
fi
set -e
cd "${WORKSPACE_ROOT}"

# Step 3: Check for changes specific to this component
echo "Step 3: Checking for changes..."
component_changes=false

# Check for changes in component-specific files
if git diff --quiet --exit-code \
        "operator/upstream-kustomizations/${COMPONENT}/" \
        "operator/pkg/manifests/${COMPONENT}/" 2>/dev/null; then
    echo "  No changes detected for ${COMPONENT}"
else
    echo "  Changes detected for ${COMPONENT}"
    component_changes=true
fi

# In local mode, just report changes and exit
if [[ "${LOCAL_MODE}" == "true" ]]; then
    if [ "${component_changes}" = "false" ]; then
        echo "  ✓ ${COMPONENT} is up to date"
        echo "${COMPONENT}:up-to-date:"
    else
        echo "  ✓ ${COMPONENT} has been updated locally"
        echo "${COMPONENT}:success:updated locally"
    fi
    exit 0
fi

# In CI mode, continue with PR creation
if [ "${component_changes}" = "false" ]; then
    echo "  ✓ ${COMPONENT} is up to date, skipping PR creation"
    echo "${COMPONENT}:up-to-date:"
    exit 0
fi

# Step 4: Create branch and PR for this component
echo "Step 4: Creating PR for ${COMPONENT}..."
BRANCH_NAME="update-upstream-manifests-${COMPONENT}"

# Delete local branch if it exists
if git show-ref --verify --quiet refs/heads/"${BRANCH_NAME}"; then
    echo "  Deleting existing local branch ${BRANCH_NAME}..."
    git branch -D "${BRANCH_NAME}"
fi

# Check if branch exists remotely
if git ls-remote --heads origin "${BRANCH_NAME}" | grep -q .; then
    echo "  Branch ${BRANCH_NAME} exists remotely, recreating from main..."
    git checkout -b "${BRANCH_NAME}" main
else
    echo "  Creating new branch ${BRANCH_NAME}..."
    git checkout -b "${BRANCH_NAME}" main
fi

# Add only component-specific changes
git add "operator/upstream-kustomizations/${COMPONENT}/" "operator/pkg/manifests/${COMPONENT}/" || true

# Check if there are actually changes to commit
if git diff --cached --quiet; then
    echo "  No changes to commit for ${COMPONENT}, skipping PR"
    echo "${COMPONENT}:no-changes:"
    git checkout main
    exit 0
fi

# Commit changes
git commit -m "chore: update upstream manifests for ${COMPONENT}

Updated upstream kustomization references and rebuilt manifests for ${COMPONENT}.

This PR was automatically created by the Update Upstream Manifests workflow." || {
    echo "  Failed to commit changes for ${COMPONENT}"
    echo "${COMPONENT}:failed:commit error"
    git checkout main
    exit 1
}

# Push branch
set +e
if git push origin "${BRANCH_NAME}" --force 2>&1; then
    echo "  ✓ Pushed branch ${BRANCH_NAME}"
else
    echo "  ✗ Failed to push branch ${BRANCH_NAME}" >&2
    echo "${COMPONENT}:failed:push error"
    git checkout main
    set -e
    exit 1
fi
set -e

# Create or update PR
PR_TITLE="chore: update upstream manifests for ${COMPONENT}"
PR_BODY="## Upstream Manifests Update: ${COMPONENT}

This PR updates upstream kustomization references and rebuilds manifests for the \`${COMPONENT}\` component.

### What changed?
- Updated upstream repository references in \`operator/upstream-kustomizations/${COMPONENT}/\` (if applicable)
- Rebuilt kustomization using \`kustomize build\`
- Updated manifest files in \`operator/pkg/manifests/${COMPONENT}/\`

### How to review
- Review the updated commit refs in \`operator/upstream-kustomizations/${COMPONENT}/**/kustomization.yaml\` files (if any)
- Review the generated manifest changes in \`operator/pkg/manifests/${COMPONENT}/\` directory

---
*This PR was automatically created by the [Update Upstream Manifests workflow](.github/workflows/update-upstream-manifests.yaml)*"

set +e
EXISTING_PR=$(gh pr view "${BRANCH_NAME}" --json number --jq .number 2>/dev/null || echo "")
set -e

if [ -n "${EXISTING_PR}" ]; then
    echo "  PR #${EXISTING_PR} already exists, updating..."
    set +e
    if gh pr edit "${EXISTING_PR}" --title "${PR_TITLE}" --body "${PR_BODY}" 2>&1; then
        echo "  ✓ Updated existing PR #${EXISTING_PR} for ${COMPONENT}"
        echo "${COMPONENT}:success:updated: PR #${EXISTING_PR}"
        git checkout main
        set -e
        exit 0
    else
        echo "  ✗ Failed to update PR #${EXISTING_PR} for ${COMPONENT}" >&2
        echo "${COMPONENT}:failed:PR update error"
        git checkout main
        set -e
        exit 1
    fi
else
    echo "  Creating new PR for ${COMPONENT}..."
    set +e
    PR_OUTPUT=$(gh pr create \
        --title "${PR_TITLE}" \
        --body "${PR_BODY}" \
        --base main \
        --head "${BRANCH_NAME}" 2>&1)
    PR_EXIT_CODE=$?
    set -e

    if [ ${PR_EXIT_CODE} -eq 0 ]; then
        PR_NUMBER=$(echo "${PR_OUTPUT}" | sed -n 's|.*pull/\([0-9]\+\).*|\1|p' | head -1)

        if [ -z "${PR_NUMBER}" ]; then
            PR_NUMBER=$(gh pr view "${BRANCH_NAME}" --json number --jq .number 2>/dev/null || echo "")
        fi

        if [ -n "${PR_NUMBER}" ]; then
            echo "  ✓ Created new PR #${PR_NUMBER} for ${COMPONENT}"
            gh pr edit "${PR_NUMBER}" --add-label "automated,dependencies" 2>/dev/null || true
            echo "${COMPONENT}:success:created: PR #${PR_NUMBER}"
            git checkout main
            exit 0
        else
            echo "  ⚠ Created PR but could not extract PR number for ${COMPONENT}"
            echo "${COMPONENT}:success:created: PR number unknown"
            git checkout main
            exit 0
        fi
    else
        echo "  ✗ Failed to create PR for ${COMPONENT}: ${PR_OUTPUT}" >&2
        echo "${COMPONENT}:failed:PR creation error"
        git checkout main
        exit 1
    fi
fi

