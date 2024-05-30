#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")" 

main() {
    local token="${1:?A token for quay should be provided}"
    local org=${2:?Quay organization name should be provided}

    create_secret "$token" "$org"
    deploy
}

create_secret() {
    local token="${1:?A token for quay should be provided}"
    local org=${2:?Quay organization name should be provided}
    local ns=image-controller
    local secret=quaytoken

    if kubectl get secret "$secret" -n "$ns" &> /dev/null; then
        echo "Image controller quay secret already exists" >&2
        return
    fi

    kubectl create secret generic "$secret" \
        --namespace="$ns" \
        --from-literal=quaytoken="$token" \
        --from-literal=organization="$org"
}

deploy() {
    echo "Deploying Image-Controller" >&2
    kubectl apply -k "${script_path}/konflux-ci/image-controller"

    echo "Waiting for Image-Controller to be ready" >&2
    retry kubectl wait --for=condition=Ready --timeout=240s -l app=image-controller -n image-controller pod
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
