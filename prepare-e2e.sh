#!/bin/bash -e

main() {
    echo "Preparing resources for E2E tests" >&2
    local quay_token="${QUAY_TOKEN:?A token for quay should be provided}"
    local quay_org=${QUAY_ORG:?Quay organization name should be provided}
    local app_private_key=${APP_PRIVATE_KEY:?GitHub App private key should be provided}
    local app_id=${APP_ID:?GitHub App ID should be provided}
    local app_webhook_secret=${APP_WEBHOOK_SECRET:?GitHub App webhook secret should be provided}
    local smee_channel=${SMEE_CHANNEL:?Link to the smee.io channel should be provided}

    ./deploy-image-controller.sh "$quay_token" "$quay_org"
    for ns in pipelines-as-code build-service integration-service; do 
        kubectl -n $ns create secret generic pipelines-as-code-secret \
        --from-literal github-private-key="$app_private_key" \
        --from-literal github-application-id="$app_id" \
        --from-literal webhook.secret="$app_webhook_secret"; done
    sed -i "s|<smee-channel>|$smee_channel|g" smee/smee-client.yaml
    kubectl create -f ./smee/smee-client.yaml
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
