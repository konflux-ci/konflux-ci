#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Deploying Konflux" >&2
    deploy

    echo "Waiting for Konflux to be ready" >&2
    local ret=0
    "${script_path}/wait-for-all.sh" || ret="$?"
    if [ $ret -ne 0 ]; then
        echo "Deployment failed"
        ./generate-err-logs.sh
    else
        echo -e "
        ***************************
         Konflux is up and running
        ***************************
        "
    fi
    exit "$ret"
}

deploy() {
    # The order is important

    # This will deploy the commos CRDs used in Konflux
    kubectl apply -k "${script_path}/konflux-ci/application-api"

    kubectl apply -k "${script_path}/konflux-ci/rbac"

    retry kubectl apply -k "${script_path}/konflux-ci/enterprise-contract"

    retry kubectl apply -k "${script_path}/konflux-ci/release" --server-side

    # The build-service depends on CRDs from the release-service
    retry kubectl apply -k "${script_path}/konflux-ci/build-service"

    # The integration-service depends on CRDs from the release-service
    retry kubectl apply -k "${script_path}/konflux-ci/integration"

    retry kubectl apply -k "${script_path}/konflux-ci/namespace-lister"

    kubectl apply -k "${script_path}/konflux-ci/ui"
    if ! kubectl get secret oauth2-proxy-client-secret -n konflux-ui; then
        kubectl get secret oauth2-proxy-client-secret --namespace=dex \
            -o yaml | grep -v '^\s*namespace:\s' \
            | kubectl apply --namespace=konflux-ui -f -
    fi
    if ! kubectl get secret oauth2-proxy-cookie-secret -n konflux-ui; then
        local cookie_secret
        # The cookie secret needs to be 16, 24, or 32 bytes long.
        # kubectl is re-encoding the value of cookie_secret, so when it's being served
        # to oauth2-proxy, it's actually the 24 bytes string which was the output of
        # openssl's encoding.
        # Need to make sure this is consistent, or find a different approach.
        cookie_secret="$(openssl rand -base64 16)"
        kubectl create secret generic oauth2-proxy-cookie-secret \
            --namespace=konflux-ui \
            --from-literal=cookie-secret="$cookie_secret"
    fi
}


retry() {
    for _ in {1..3}; do
        local ret=0
        "$@" || ret="$?"
        if [[ "$ret" -eq 0 ]]; then
            return 0
        fi
        sleep 3
    done

    return "$ret"
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
