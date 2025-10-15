#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"
repo_root="$(dirname -- "${script_path}")"

main() {
    echo "🔍 Checking requirements" >&2
    check_req
    echo "🧪 Testing PVC creation for default storage class" >&2
    test_pvc_binding
    echo "🌊 Deploying Konflux Dependencies" >&2
    deploy
    echo "⏳ Waiting for the dependencies to be ready" >&2
    "${script_path}/wait-for-all.sh"
}

check_req(){
    # declare the requirements
    local requirements=(kubectl openssl)
    local uninstalled_requirements=()

    # check if requirements are installed
    for i in "${requirements[@]}"; do
        if ! command -v "$i" &> /dev/null; then
                if [ "$i" == "kubectl" ]; then
                    uninstalled_requirements+=("kubectl (server-side support required - v1.31.1 or newer)")
                else
                    uninstalled_requirements+=("$i")
                fi
        else
                echo -e "$i is installed"
        fi
    done

    # check if kubectl has server-side support
    if command -v kubectl &> /dev/null; then
        echo -e "Checking if kubectl has server-side support"
        if ! kubectl apply --help | grep -q -- --server-side; then
            uninstalled_requirements+=("kubectl (server-side support required - v1.31.1 or newer)")
        else
            echo -e "kubectl supports server-side apply"
        fi
    fi

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
    echo "🔐 Deploying Cert Manager..." >&2
    deploy_cert_manager
    echo "🤝 Deploying Trust Manager..." >&2
    deploy_trust_manager
    echo "📜 Setting up Cluster Issuer..." >&2
    kubectl apply -k "${repo_root}/dependencies/cluster-issuer"
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
    echo "📋 Deploying Konflux Info..." >&2
    deploy_konflux_info
}

test_pvc_binding(){
    local pvc_resources="${repo_root}/dependencies/pre-deployment-pvc-binding"
    echo "Creating PVC from '$pvc_resources' using the cluster's default storage class"
    sleep 10  # Retries are not enough to ensure the default SA is created, see https://github.com/konflux-ci/konflux-ci/issues/161
    retry "kubectl apply -k ${pvc_resources}" "Error while creating pre-deployment-pvc-binding"
    retry "kubectl wait --for=jsonpath={status.phase}=Bound pvc/test-pvc -n test-pvc-ns --timeout=20s" \
          "Test PVC unable to bind on default storage class"
    kubectl delete -k "$pvc_resources"
    echo "PVC binding successfull"
}

deploy_tekton() {
    echo "  🐱 Installing Tekton Operator..." >&2
    # Operator
    kubectl apply -k "${repo_root}/dependencies/tekton-operator"
    retry "kubectl wait --for=condition=Ready -l app=tekton-operator -n tekton-operator pod --timeout=240s" \
          "Tekton Operator did not become available within the allocated time"
    # Wait for the operator to create configs for the first time before applying configs
    retry "kubectl wait --for=condition=Ready tektonconfig/config --timeout=360s" \
          "Tekton Config resource was not created within the allocated time"
    echo "  ⚙️  Configuring Tekton..." >&2
    retry "kubectl apply -k ${repo_root}/dependencies/tekton-config" \
          "The Tekton Config resource was not updated within the allocated time"

    echo "  🔄 Setting up Pipeline As Code..." >&2
    kubectl apply -k "${repo_root}/dependencies/pipelines-as-code"

    # Wait for the operator to reconcile after applying the configs
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=60s

    echo "  🔐 Setting up Tekton Chains RBAC..." >&2
    kubectl apply -k "${script_path}/dependencies/tekton-chains-rbac"

    echo "  📊 Setting up Tekton Results..." >&2
    if ! kubectl get secret tekton-results-postgres -n tekton-pipelines; then
        echo "🔑 Creating secret tekton-results-postgres" >&2
        local db_password
        db_password="$(openssl rand -base64 20)"
        kubectl create secret generic tekton-results-postgres \
            --namespace="tekton-pipelines" \
            --from-literal=POSTGRES_USER=postgres \
            --from-literal=POSTGRES_PASSWORD="$db_password"
    fi
    kubectl apply -k "${repo_root}/dependencies/tekton-results"

    # Wait for tekton-results-api to be ready before the watcher starts connecting
    # This prevents a race condition where the watcher crashes if the API isn't ready yet
    echo "  ⏳ Waiting for tekton-results-api to be ready..." >&2
    retry "kubectl wait --for=condition=Available --timeout=120s deployment/tekton-results-api -n tekton-pipelines" \
          "tekton-results-api did not become available within the allocated time"
}

deploy_cert_manager() {
    kubectl apply -k "${repo_root}/dependencies/cert-manager"
    sleep 5
    retry "kubectl wait --for=condition=Ready --timeout=120s -l app.kubernetes.io/instance=cert-manager -n cert-manager pod" \
          "Cert manager did not become available within the allocated time"
}

deploy_trust_manager() {
    kubectl apply -k "${repo_root}/dependencies/trust-manager"
    sleep 5
    retry "kubectl wait --for=condition=Ready --timeout=60s -l app.kubernetes.io/instance=trust-manager -n cert-manager pod" \
          "Trust manager did not become available within the allocated time"
}

deploy_dex() {
    kubectl apply -k "${repo_root}/dependencies/dex"
    if ! kubectl get secret oauth2-proxy-client-secret -n dex; then
        echo "🔑 Creating secret oauth2-proxy-client-secret" >&2
        local client_secret
        client_secret="$(openssl rand -base64 20 | tr '+/' '-_' | tr -d '\n' | tr -d '=')"
        kubectl create secret generic oauth2-proxy-client-secret \
            --namespace=dex \
            --from-literal=client-secret="$client_secret"
    fi
}

deploy_registry() {
    kubectl apply -k "${repo_root}/dependencies/registry"
    retry "kubectl wait --for=condition=Ready --timeout=240s -n kind-registry -l run=registry pod" \
          "The local registry did not become available within the allocated time"
}

deploy_smee() {
    local patch="${repo_root}/dependencies/smee/smee-channel-id.yaml"
    if [ ! -f "$patch" ]; then
        echo "Randomizing smee-channel ID"
        local channel_id
        local placeholder=CHANNELID
        local template="${repo_root}/dependencies/smee/smee-channel-id.tpl"
        channel_id="$(head -c 30 /dev/random | base64 | tr -dc 'a-zA-Z0-9')"
        sed "s/$placeholder/$channel_id/g" "$template" > "$patch"
    fi
    kubectl apply -k "${repo_root}/dependencies/smee"
}

deploy_kyverno() {
    kubectl apply -k "${repo_root}/dependencies/kyverno" --server-side
    # Wait for policy CRD to be installed. Don't need to wait for everything to be up
    sleep 5
    kubectl apply -k "${repo_root}/dependencies/kyverno/policy"
}

deploy_konflux_info() {
    kubectl apply -k "${script_path}/dependencies/konflux-info"
}

retry() {
    for i in {1..3}; do
        local ret=0
        $1 || ret="$?"
        if [[ "$ret" -eq 0 ]]; then
            return 0
        fi
        if [[ "$i" -lt 3 ]]; then
            echo "🔄 Retrying command (attempt $((i+1))/3)..." >&2
            sleep 3
        fi
    done

    echo "$1": "$2."
    return "$ret"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
