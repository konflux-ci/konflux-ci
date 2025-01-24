#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Checking requirements" >&2
    check_req
    echo "Testing PVC creation for default storage class" >&2
    test_pvc_binding
    echo "Deploying Konflux Dependencies" >&2
    deploy
    echo "Waiting for the dependencies to be ready" >&2
    "${script_path}/wait-for-all.sh"
}

check_req(){
    # declare the requirements
    local requirements=(kubectl openssl)
    local uninstalled_requirements=()

    # check if requirements are installed
    for i in "${requirements[@]}"; do
        if ! command -v "$i" &> /dev/null; then
                uninstalled_requirements+=("$i")
        else
                echo -e "$i is installed"
        fi  
    done

    if (( ${#uninstalled_requirements[@]} == 0 )); then
        echo -e "\nAll requirements are met\nContinue"
    else
        echo -e "\nSome requirements are missing, please install the following requirements first:"
        for req in "${uninstalled_requirements[@]}"; do
                echo "$req"
        done
        exit 1
    fi
}

deploy() {
    deploy_cert_manager
    deploy_trust_manager
    kubectl apply -k "${script_path}/dependencies/cluster-issuer"
    deploy_tekton
    deploy_dex
    deploy_registry
    deploy_smee
}

test_pvc_binding(){
    local pvc_resources="${script_path}/dependencies/pre-deployment-pvc-binding"
    echo "Creating PVC from '$pvc_resources' using the cluster's default storage class"
    sleep 10  # Retries are not enough to ensure the default SA is created, see https://github.com/konflux-ci/konflux-ci/issues/161
    retry "kubectl apply -k ${pvc_resources}" "Error while creating pre-deployment-pvc-binding"
    retry "kubectl wait --for=jsonpath={status.phase}=Bound pvc/test-pvc -n test-pvc-ns --timeout=20s" \
          "Test PVC unable to bind on default storage class"
    kubectl delete -k "$pvc_resources"
    echo "PVC binding successfull"
}

deploy_tekton() {
    # Operator
    kubectl apply -k "${script_path}/dependencies/tekton-operator"
    retry "kubectl wait --for=condition=Ready -l app=tekton-operator -n tekton-operator pod --timeout=240s" \
          "Tekton Operator did not become available within the allocated time"
    # Wait for the operator to create configs for the first time before applying configs
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=360s
    retry "kubectl apply -k ${script_path}/dependencies/tekton-config" \
          "The Tekton Config resource was not updated within the allocated time"

    # Pipeline As Code
    kubectl apply -k "${script_path}/dependencies/pipelines-as-code"

    # Wait for the operator to reconcile after applying the configs
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=60s

    # Tekton Results
    if ! kubectl get secret tekton-results-postgres -n tekton-pipelines; then
        local db_password
        db_password="$(openssl rand -base64 20)"
        kubectl create secret generic tekton-results-postgres \
            --namespace="tekton-pipelines" \
            --from-literal=POSTGRES_USER=postgres \
            --from-literal=POSTGRES_PASSWORD="$db_password"
    fi
    kubectl apply -k "${script_path}/dependencies/tekton-results"
}

deploy_cert_manager() {
    kubectl apply -k "${script_path}/dependencies/cert-manager"
    sleep 5
    retry "kubectl wait --for=condition=Ready --timeout=120s -l app.kubernetes.io/instance=cert-manager -n cert-manager pod" \
          "Cert manager did not become available within the allocated time"
}

deploy_trust_manager() {
    kubectl apply -k "${script_path}/dependencies/trust-manager"
    sleep 5
    retry "kubectl wait --for=condition=Ready --timeout=60s -l app.kubernetes.io/instance=trust-manager -n cert-manager pod" \
          "Trust manager did not become available within the allocated time"
}

deploy_dex() {
    kubectl apply -k "${script_path}/dependencies/dex"
    if ! kubectl get secret oauth2-proxy-client-secret -n dex; then
        local client_secret
        client_secret="$(openssl rand -base64 20 | tr '+/' '-_' | tr -d '\n' | tr -d '=')"
        kubectl create secret generic oauth2-proxy-client-secret \
            --namespace=dex \
            --from-literal=client-secret="$client_secret"
    fi
}

deploy_registry() {
    kubectl apply -k "${script_path}/dependencies/registry"
    retry "kubectl wait --for=condition=Ready --timeout=240s -n kind-registry -l run=registry pod" \
          "The local registry did not become available within the allocated time"
}

deploy_smee() {
    local patch="${script_path}/dependencies/smee/smee-channel-id.yaml"
    if [ ! -f "$patch" ]; then
        echo "Randomizing smee-channel ID"
        local channel_id
        local placeholder=CHANNELID
        local template="${script_path}/dependencies/smee/smee-channel-id.tpl"
        channel_id="$(head -c 30 /dev/random | base64 | tr -dc 'a-zA-Z0-9')"
        sed "s/$placeholder/$channel_id/g" "$template" > "$patch"
    fi
    kubectl apply -k "${script_path}/dependencies/smee"
}

retry() {
    for _ in {1..3}; do
        local ret=0
        $1 || ret="$?"
        if [[ "$ret" -eq 0 ]]; then
            return 0
        fi
        sleep 3
    done

    echo "$1": "$2."
    return "$ret"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
