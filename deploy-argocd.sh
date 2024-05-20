#!/bin/bash -e

# Deploy Applications to ArgoCD on EKS Cluster

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

main() {
    verify_permissions || exit $?
    login_argocd
    verify_pvc_binding
    deploy_apps
}

verify_permissions() {
    if [ "$(kubectl auth can-i '*' '*' --all-namespaces)" != "yes" ]; then
        echo
        echo "[ERROR] User '$(oc whoami)' does not have the required 'cluster-admin' role." 1>&2
        echo "Log into the cluster with a user with the required privileges (e.g. kubeadmin) and retry."
        return 1
    fi
}

login_argocd() {
    local ARGOCD_SERVER="argocd.konflux-ci.net"
    argocd login $ARGOCD_SERVER --sso
}

verify_pvc_binding(){
    local pvc_resources="${ROOT}/dependencies/pre-deployment-pvc-binding"
    echo "Creating PVC from '$pvc_resources' using the cluster's default storage class"
    kubectl apply -k "$pvc_resources"
    echo "PVC binding successfull"
}

deploy_tekton_secret() {
    if ! kubectl get secret tekton-results-postgres -n tekton-pipelines; then
        local db_password
        db_password="$(openssl rand -base64 20)"
        kubectl create secret generic tekton-results-postgres \
            --namespace="tekton-pipelines" \
            --from-literal=POSTGRES_USER=postgres \
            --from-literal=POSTGRES_PASSWORD="$db_password"
    fi
}

deploy_keycloak_secret() {
    if ! kubectl get secret keycloak-db-secret -n keycloak; then
        local db_password
        db_password="$(openssl rand -base64 20)"
        kubectl create secret generic keycloak-db-secret \
            --namespace=keycloak \
            --from-literal=POSTGRES_USER=postgres \
            --from-literal=POSTGRES_PASSWORD="$db_password"
    fi
}

deploy_apps() {
    echo "Deploying applications"

    kubectl apply -k "${ROOT}/argo-cd-apps/base/dependencies"
    deploy_tekton_secret
    deploy_keycloak_secret

    kubectl apply -k "${ROOT}/argo-cd-apps/base/konflux-ci"
    echo "Applications deployed"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
