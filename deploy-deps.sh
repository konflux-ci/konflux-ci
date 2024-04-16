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


    retry kubectl apply -k "${script_path}/dependencies/tekton-config"


    kubectl create -k "${script_path}/dependencies/pipelines-as-code"
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=120s

    kubectl create secret generic tekton-results-postgres \
        --namespace="tekton-pipelines" \
        --from-literal=POSTGRES_USER=postgres \
        --from-literal=POSTGRES_PASSWORD="$(openssl rand -base64 20)"
    kubectl create -k "${script_path}/dependencies/tekton-results"

    kubectl create -k "${script_path}/dependencies/ingress-nginx"
    deploy_keycloak
}

deploy_cert_manager() {
    kubectl create -k "${script_path}/dependencies/cert-manager"
    kubectl wait --for=condition=Ready --timeout=120s -l app.kubernetes.io/instance=cert-manager -n cert-manager pod
}

deploy_keycloak() {
    kubectl create -k "${script_path}/dependencies/keycloak/crd"
    sleep 2 # Give the api time to realize it support new CRDs
    kubectl create -k "${script_path}/dependencies/keycloak/deployment"
    kubectl create secret generic keycloak-db-secret \
        --namespace=keycloak \
        --from-literal=POSTGRES_USER=postgres \
        --from-literal=POSTGRES_PASSWORD="$(openssl rand -base64 20)"

    local ret=0
    kubectl wait --for=condition=Ready --timeout=120s  -n keycloak keycloak/keycloak || ret="$?"

    if [[ ret -ne 0 ]]; then
        kubectl get -o yaml keycloak/keycloak -n keycloak
        kubectl get pod -n keycloak
        kubectl logs -l app.kubernetes.io/name=keycloak-operator -n keycloak 
        return "$ret"
    fi

    kubectl wait --for=condition=Ready --timeout=120s -l app=postgresql-db -n keycloak pod
    kubectl wait --for=condition=Ready --timeout=120s -l app=keycloak -n keycloak pod
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
