#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Deploying Konflux Dependencies" >&2
    deploy
    
    echo "Waiting for the dependencies to be ready" >&2
    "${script_path}/wait-for-all-.sh"
    
}

deploy() {
    kubectl create -k "${script_path}/dependencies/tekton-operator"
    kubectl create -k "${script_path}dependencies/tekton-config"
    kubectl create -k "${script_path}dependencies/pipelines-as-code"
    kubectl create -k "${script_path}dependencies/cert-manager"
    kubectl create -k "${script_path}dependencies/ingress-nginx"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
