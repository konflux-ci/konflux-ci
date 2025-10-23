#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"
repo_root="$(dirname -- "${script_path}")"

main() {
    local token="${1:?A token for quay should be provided}"
    local org=${2:?Quay organization name should be provided}
    echo "💧 Starting Image Controller deployment..." >&2
    deploy
    echo "🔑 Setting up Quay credentials..." >&2
    create_secret "$token" "$org"
    echo "⏳ Waiting for Image Controller to be ready..." >&2
    wait
}

deploy() {
    echo "🌊 Deploying Image Controller components..." >&2
    kubectl apply -k "${repo_root}/konflux-ci/image-controller"
}

create_secret() {
    local token="${1:?A token for quay should be provided}"
    local org=${2:?Quay organization name should be provided}
    local ns=image-controller
    local secret=quaytoken

    if kubectl get secret "$secret" -n "$ns" &> /dev/null; then
        echo "🔒 Image controller quay secret already exists" >&2
        return
    fi

    echo "🔑 Creating new Quay secret..." >&2
    kubectl create secret generic "$secret" \
        --namespace="$ns" \
        --from-literal=quaytoken="$token" \
        --from-literal=organization="$org"
}

wait() {
    echo "⏳ Waiting for Image Controller pods to be ready..." >&2
    retry "kubectl wait --for=condition=Ready --timeout=240s -l control-plane=controller-manager -n image-controller pod" \
          "Image-Controller did not become available within the allocated time"
}
    


retry() {
    for _ in {1..3}; do
        local ret=0
        $1 || ret="$?"
        if [[ "$ret" -eq 0 ]]; then
            return 0
        fi
        echo "Retrying"
        sleep 3
    done

    echo "$1": "$2."
    return "$ret"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
