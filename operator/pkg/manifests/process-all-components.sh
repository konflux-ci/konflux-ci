#!/bin/bash
#
# Process all components: update upstream refs, build kustomizations, and create PRs
#
# Usage: process-all-components.sh <workspace-root>
#
# Outputs JSON results to /tmp/component-results.json
# Exit codes: 0 = all succeeded or up-to-date, 1 = one or more failed

set -euo pipefail

WORKSPACE_ROOT="${1:-}"

if [[ -z "${WORKSPACE_ROOT}" ]]; then
    echo "Error: Workspace root is required" >&2
    echo "Usage: $0 <workspace-root>" >&2
    exit 1
fi

cd "${WORKSPACE_ROOT}"

# List of components to process
COMPONENTS=(
    "application-api"
    "build-service"
    "enterprise-contract"
    "image-controller"
    "integration"
    "namespace-lister"
    "rbac"
    "release"
    "ui"
)

# Track results
declare -a failed_components=()
declare -a successful_components=()
declare -a up_to_date_components=()
declare -A component_messages

# Process each component
for component in "${COMPONENTS[@]}"; do
    echo ""
    echo "========================================="
    echo "Processing component: ${component}"
    echo "========================================="

    # Ensure we're on main branch and clean before each component
    git checkout main
    git reset --hard origin/main
    git clean -fd

    # Run the component processing script
    set +e
    SCRIPT_OUTPUT=$(bash operator/pkg/manifests/process-component.sh "${component}" "${WORKSPACE_ROOT}" 2>&1)
    set -e

    # Display all output
    echo "${SCRIPT_OUTPUT}"

    # Extract the result line (format: "component:status:message")
    RESULT_LINE=$(echo "${SCRIPT_OUTPUT}" | grep -E "^${component}:(success|failed|up-to-date|no-changes):" | tail -1)

    if [[ -z "${RESULT_LINE}" ]]; then
        # If we didn't get a result line, treat as failed
        echo "  ⚠ Warning: Could not parse result for ${component}, treating as failed" >&2
        failed_components+=("${component}")
        component_messages["${component}"]="script output parsing error"
        continue
    fi

    # Parse the result (component name is already known from the loop, so we discard it with _)
    IFS=':' read -r _ status message <<< "${RESULT_LINE}" || true

    if [[ "${status}" == "success" ]]; then
        successful_components+=("${component}")
        component_messages["${component}"]="${message}"
    elif [[ "${status}" == "failed" ]]; then
        failed_components+=("${component}")
        component_messages["${component}"]="${message}"
    elif [[ "${status}" == "up-to-date" ]] || [[ "${status}" == "no-changes" ]]; then
        up_to_date_components+=("${component}")
        component_messages["${component}"]="${status}"
    else
        echo "  ⚠ Warning: Unknown status '${status}' for ${component}" >&2
        failed_components+=("${component}")
        component_messages["${component}"]="unknown status: ${status}"
    fi
done

# Generate JSON output using jq (available in GitHub Actions runners)
# Build JSON structure with proper escaping
jq -n \
    --argjson failed "$(
        for component in "${failed_components[@]}"; do
            message="${component_messages[$component]}"
            jq -n --arg comp "${component}" --arg msg "${message}" '{component: $comp, message: $msg}'
        done | jq -s .
    )" \
    --argjson successful "$(
        for component in "${successful_components[@]}"; do
            message="${component_messages[$component]}"
            jq -n --arg comp "${component}" --arg msg "${message}" '{component: $comp, message: $msg}'
        done | jq -s .
    )" \
    --argjson up_to_date "$(
        for component in "${up_to_date_components[@]}"; do
            message="${component_messages[$component]}"
            jq -n --arg comp "${component}" --arg msg "${message}" '{component: $comp, message: $msg}'
        done | jq -s .
    )" \
    '{
        failed: $failed,
        successful: $successful,
        up_to_date: $up_to_date
    }' > /tmp/component-results.json

# Print summary
echo ""
echo "========================================="
echo "Summary"
echo "========================================="
# Get array lengths safely (arrays are initialized, so this is safe)
successful_count=${#successful_components[@]}
failed_count=${#failed_components[@]}
up_to_date_count=${#up_to_date_components[@]}

echo "Successful components: ${successful_count}"
for comp in "${successful_components[@]}"; do
    echo "  ✓ ${comp}: ${component_messages[$comp]}"
done
echo ""
echo "Failed components: ${failed_count}"
for comp in "${failed_components[@]}"; do
    echo "  ✗ ${comp}: ${component_messages[$comp]}"
done
echo ""
echo "Up-to-date components: ${up_to_date_count}"
for comp in "${up_to_date_components[@]}"; do
    echo "  ○ ${comp}: ${component_messages[$comp]}"
done

# Exit with error if any component failed
if [ "${failed_count}" -gt 0 ]; then
    echo ""
    echo "⚠️ Some components failed to process. Check logs above for details."
    exit 1
fi

exit 0

