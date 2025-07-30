#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"
IS_OPENSHIFT=false

main() {
    echo "🔍 Checking requirements" >&2
    check_req
    detect_platform
    echo "🧪 Testing PVC creation for default storage class" >&2
    test_pvc_binding
    echo "🌊 Deploying Konflux Dependencies" >&2
    deploy
    echo "⏳ Waiting for the dependencies to be ready" >&2
    "${script_path}/wait-for-all.sh"
}

check_req(){
    local requirements=(kubectl openssl)
    local uninstalled_requirements=()

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

detect_platform() {
    if kubectl get clusterversion version &> /dev/null; then
        echo "🎯 Detected OpenShift environment"
        IS_OPENSHIFT=true
    else
        echo "🎯 Detected Kubernetes environment"
        IS_OPENSHIFT=false
    fi
}

sanitize_for_openshift() {
    local dir="$1"
    if [ "$IS_OPENSHIFT" = true ]; then
        echo "🔧 Sanitizing manifests for OpenShift in: $dir"
        find "$dir" -type f -name '*.yaml' -o -name '*.yml' | while read -r file; do
            sed -i '/runAsUser:/d;/runAsGroup:/d;/securityContext: *{ *}/d' "$file"
        done
    fi
}

deploy() {
    echo "🔐 Deploying Cert Manager..." >&2
    deploy_cert_manager
    echo "🤝 Deploying Trust Manager..." >&2
    deploy_trust_manager
    echo "📜 Setting up Cluster Issuer..." >&2
    sanitize_for_openshift "${script_path}/dependencies/cluster-issuer"
    kubectl apply -k "${script_path}/dependencies/cluster-issuer"
    echo "🐱 Deploying Tekton..." >&2
    deploy_tekton
    echo "🔑 Deploying Dex..." >&2
    deploy_dex
    echo "📦 Deploying Registry..." >&2
    deploy_registry
    echo "🔄 Deploying Smee..." >&2
    deploy_smee
    echo "🛡️  Deploying Kyverno..." >&2
    deploy_kyverno
}

test_pvc_binding(){
    local pvc_resources="${script_path}/dependencies/pre-deployment-pvc-binding"
    echo "Creating PVC from '$pvc_resources' using the cluster's default storage class"
    sleep 10
    retry "kubectl apply -k ${pvc_resources}" "Error while creating pre-deployment-pvc-binding"
    retry "kubectl wait --for=jsonpath={status.phase}=Bound pvc/test-pvc -n test-pvc-ns --timeout=20s" \
          "Test PVC unable to bind on default storage class"
    kubectl delete -k "$pvc_resources"
    echo "PVC binding successful"
}

deploy_cert_manager() {
    local dir="${script_path}/dependencies/cert-manager"
    sanitize_for_openshift "$dir"
    kubectl apply -k "$dir"
    sleep 5
    retry "kubectl wait --for=condition=Ready --timeout=120s -l app.kubernetes.io/instance=cert-manager -n cert-manager pod" \
          "Cert manager did not become available within the allocated time"
}

deploy_trust_manager() {
    local dir="${script_path}/dependencies/trust-manager"
    sanitize_for_openshift "$dir"
    kubectl apply -k "$dir"
    sleep 5
    retry "kubectl wait --for=condition=Ready --timeout=60s -l app.kubernetes.io/instance=trust-manager -n cert-manager pod" \
          "Trust manager did not become available within the allocated time"
}

deploy_tekton() {
    echo "  🐱 Installing Tekton Operator..." >&2
    local operator_dir="${script_path}/dependencies/tekton-operator"
    sanitize_for_openshift "$operator_dir"
    kubectl apply -k "$operator_dir"
    retry "kubectl wait --for=condition=Ready -l app=tekton-operator -n tekton-operator pod --timeout=240s" \
          "Tekton Operator did not become available within the allocated time"

    kubectl wait --for=condition=Ready tektonconfig/config --timeout=360s

    echo "  ⚙️  Configuring Tekton..." >&2
    local config_dir="${script_path}/dependencies/tekton-config"
    sanitize_for_openshift "$config_dir"
    retry "kubectl apply -k $config_dir" "The Tekton Config resource was not updated"

    echo "  🔄 Setting up Pipeline As Code..." >&2
    local pac_dir="${script_path}/dependencies/pipelines-as-code"
    sanitize_for_openshift "$pac_dir"
    kubectl apply -k "$pac_dir"

    kubectl wait --for=condition=Ready tektonconfig/config --timeout=60s

    echo "  📊 Setting up Tekton Results..." >&2
    if ! kubectl get secret tekton-results-postgres -n tekton-pipelines; then
        local db_password
        db_password="$(openssl rand -base64 20)"
        kubectl create secret generic tekton-results-postgres \
            --namespace="tekton-pipelines" \
            --from-literal=POSTGRES_USER=postgres \
            --from-literal=POSTGRES_PASSWORD="$db_password"
    fi
    local results_dir="${script_path}/dependencies/tekton-results"
    sanitize_for_openshift "$results_dir"
    kubectl apply -k "$results_dir"
}

deploy_dex() {
    local dir="${script_path}/dependencies/dex"
    sanitize_for_openshift "$dir"
    kubectl apply -k "$dir"
    if ! kubectl get secret oauth2-proxy-client-secret -n dex; then
        local client_secret
        client_secret="$(openssl rand -base64 20 | tr '+/' '-_' | tr -d '\n' | tr -d '=')"
        kubectl create secret generic oauth2-proxy-client-secret \
            --namespace=dex \
            --from-literal=client-secret="$client_secret"
    fi
}

deploy_registry() {
    local dir="${script_path}/dependencies/registry"
    sanitize_for_openshift "$dir"
    kubectl apply -k "$dir"
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
    local dir="${script_path}/dependencies/smee"
    sanitize_for_openshift "$dir"
    kubectl apply -k "$dir"
}

deploy_kyverno() {
    local base="${script_path}/dependencies/kyverno"
    local policy="${script_path}/dependencies/kyverno/policy"
    sanitize_for_openshift "$base"
    sanitize_for_openshift "$policy"
    kubectl apply -k "$base" --server-side
    sleep 5
    kubectl apply -k "$policy"
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
