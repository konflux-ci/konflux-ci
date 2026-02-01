#!/bin/bash -e
#
# DEPRECATED: This script is deprecated and will be removed in a future release.
# The operator now deploys image-controller, and scripts/deploy-local.sh creates
# the Quay secret directly.
#
# This script remains for compatibility with the main branch CI workflow
# (operator-test-e2e.yaml uses pull_request_target which runs workflow from main).
# Once the updated workflow merges to main, this script can be deleted.
#

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    local token="${1:?A token for quay should be provided}"
    local org=${2:?Quay organization name should be provided}
    echo "💧 Starting Image Controller deployment..." >&2

    # Check if operator is managing image-controller
    if is_operator_managed; then
        echo "🤖 Operator is managing image-controller, skipping manifest deployment..." >&2
    else
        deploy
    fi

    echo "🔑 Setting up Quay credentials..." >&2
    create_secret "$token" "$org"
    echo "⏳ Waiting for Image Controller to be ready..." >&2
    wait
}

# Check if the operator is managing image-controller
# Returns 0 (true) if KonfluxImageController CR exists, 1 (false) otherwise
is_operator_managed() {
    # Check if KonfluxImageController CR exists
    if kubectl get konfluximagecontroller konflux-image-controller &> /dev/null; then
        return 0
    fi

    # Also check if Konflux CR has image-controller enabled
    local enabled
    enabled=$(kubectl get konflux konflux -o jsonpath='{.spec.imageController.enabled}' 2>/dev/null || echo "")
    if [[ "$enabled" == "true" ]]; then
        return 0
    fi

    return 1
}

deploy() {
    echo "🌊 Deploying Image Controller components..." >&2
    kubectl apply -k "${script_path}/konflux-ci/image-controller"
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
