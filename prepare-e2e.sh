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
    kubectl run clair -n default --attach=true --restart=Never --image quay.io/redhat-appstudio/clair-in-ci:v1 --command -- "clair-action" "report" "--image-ref=quay.io/psturc_org/user-ns2/konflux-ci-upstream/konflux-ci-upstream@sha256:06ed74b1a0e4cce488ef5f831fcd403385bda21ad2ec27d034b8c1a54ed59fb2" "--db-path=/tmp/matcher.db" "--format=quay"
    main "$@"
fi
