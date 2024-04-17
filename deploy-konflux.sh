#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")" 

main() {
    echo "Deploying Konflux" >&2
    deploy

    echo "Waiting for Konflux to be ready" >&2
    local ret=0
    "${script_path}/wait-for-all.sh" || ret="$?"
    kubectl describe deployment proxy -n konflux-ui
    kubectl logs deployment/proxy -n konflux-ui --all-containers=true --tail=10
    exit "$ret"
}

deploy() {
    # The order is important

    # This will deploy the commos CRDs used in Konflux
    kubectl apply -k "${script_path}/konflux-ci/application-api"

    kubectl apply -k "${script_path}/konflux-ci/rbac"

    kubectl apply -k "${script_path}/konflux-ci/enterprise-contract/core"

    kubectl apply -k "${script_path}/konflux-ci/release"

    # The build-service depends on CRDs from the release-service
    kubectl apply -k "${script_path}/konflux-ci/build-service"

    # The integration-service depends on CRDs from the release-service
    kubectl apply -k "${script_path}/konflux-ci/integration"

    kubectl apply -k "${script_path}/konflux-ci/ui"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
