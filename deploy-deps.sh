#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Deploying Konflux Dependencies" >&2
    deploy
    
    echo "Waiting for the dependencies to be ready" >&2
    "${script_path}/wait-for-all.sh"
    
}

deploy() {
    deploy_cert_manager

    kubectl create -k "${script_path}/dependencies/cluster-issuer"
    kubectl create -k "${script_path}/dependencies/tekton-operator"
    kubectl create -k "${script_path}/dependencies/tekton-config"
    kubectl create -k "${script_path}/dependencies/pipelines-as-code"
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=120s

    kubectl create secret generic tekton-results-postgres \
        --namespace="tekton-pipelines" \
        --from-literal=POSTGRES_USER=postgres \
        --from-literal=POSTGRES_PASSWORD="$(openssl rand -base64 20)"
    kubectl create -k "${script_path}/dependencies/tekton-results"

    kubectl create -k "${script_path}/dependencies/ingress-nginx"
}

deploy_cert_manager() {
    kubectl create -k "${script_path}/dependencies/cert-manager"
    kubectl wait --for=condition=Ready --timeout=120s -l app.kubernetes.io/instance=cert-manager -n cert-manager pod
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
