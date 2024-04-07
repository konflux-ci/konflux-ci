#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")" 

main() {
    echo "Deploying Konflux" >&2
    deploy

    echo "Waiting for Konflux to be ready" >&2
    "${script_path}/wait-for-all-.sh"
}

deploy() {
    # The order is important

    kubectl create -k "${script_path}/dependencies/cluster-issuer"

    # This will deploy the commos CRDs used in Konflux
    kubectl create -k "${script_path}/konflux-ci/application-api"

    kubectl create -k "${script_path}/dependencies/rbac"

    kubectl create -k "${script_path}/konflux-ci/enterprise-contract/core"

    kubectl create -k "${script_path}/konflux-ci/release"

    # The build-service depends on CRDs from the release-service
    kubectl create -k "${script_path}/konflux-ci/build-service"

    # The integration-service depends on CRDs from the release-service
    kubectl create -k "${script_path}/konflux-ci/integration"

    kubectl create -k "${script_path}/konflux-ci/ui"
    if [[ "$KONFLUX_PULL_SECRET" ]]; then
        echo "$KONFLUX_PULL_SECRET" | kubectl create -n konflux-ui -f -
    fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
