#!/bin/bash -e
#
# DEPRECATED: This script is deprecated and will be removed in a future release.
# All functionality has been consolidated into scripts/deploy-local.sh
#
# This script remains for compatibility with the main branch CI workflow
# (operator-test-e2e.yaml uses pull_request_target which runs workflow from main).
# Once the updated workflow merges to main, this script can be deleted.
#

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Preparing resources for E2E tests" >&2
    local quay_token="${QUAY_TOKEN:?A token for quay should be provided}"
    local quay_org=${QUAY_ORG:?Quay organization name should be provided}
    local app_private_key=${APP_PRIVATE_KEY:?GitHub App private key should be provided}
    local app_id=${APP_ID:?GitHub App ID should be provided}
    local app_webhook_secret=${APP_WEBHOOK_SECRET:?GitHub App webhook secret should be provided}
    local smee_channel=${SMEE_CHANNEL:?Link to the smee.io channel should be provided}

    # Enable image-controller in the Konflux CR before deploying (if operator is present)
    if kubectl get konflux konflux &>/dev/null; then
        echo "Enabling image-controller in Konflux CR..." >&2
        kubectl patch konflux konflux --type=merge -p '{"spec":{"imageController":{"enabled":true}}}'
    else
        echo "Konflux CR not found, skipping operator-managed image-controller setup..." >&2
    fi

    "${script_path}/../../deploy-image-controller.sh" "$quay_token" "$quay_org"
    for ns in pipelines-as-code build-service integration-service; do
        kubectl -n $ns create secret generic pipelines-as-code-secret \
        --from-literal github-private-key="$app_private_key" \
        --from-literal github-application-id="$app_id" \
        --from-literal webhook.secret="$app_webhook_secret"; done
    sed "s|https://smee.io/CHANNELID|$smee_channel|g" \
        "${script_path}/../../dependencies/smee/smee-channel-id.tpl" \
        > "${script_path}/../../dependencies/smee/smee-channel-id.yaml"
    kubectl apply -k "${script_path}/../../dependencies/smee"
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
