#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

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
    local min_kubectl_version="v1.31.4"

    # check if requirements are installed
    for i in "${requirements[@]}"; do
        if ! command -v "$i" &> /dev/null; then
                if [ "$i" == "kubectl" ]; then
                    uninstalled_requirements+=("kubectl (${min_kubectl_version} or newer)")
                else
                    uninstalled_requirements+=("$i")
                fi
        else
                echo -e "$i is installed"
        fi
    done

    # check kubectl version
    if command -v kubectl &> /dev/null; then
        echo -e "Checking kubectl version"
        local kubectl_version
        kubectl_version=$(kubectl version --client 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)

        if [[ -z "$kubectl_version" ]]; then
            uninstalled_requirements+=("kubectl (${min_kubectl_version} or newer, found unknown)")
        else
            # Parse version components
            local min_major min_minor min_patch cur_major cur_minor cur_patch
            IFS='.' read -r min_major min_minor min_patch <<< "${min_kubectl_version#v}"
            IFS='.' read -r cur_major cur_minor cur_patch <<< "${kubectl_version#v}"

            # Compare using arithmetic
            if (( cur_major > min_major )) || \
               (( cur_major == min_major && cur_minor > min_minor )) || \
               (( cur_major == min_major && cur_minor == min_minor && cur_patch >= min_patch )); then
                echo -e "kubectl version ${kubectl_version} meets minimum requirement (${min_kubectl_version})"
            else
                uninstalled_requirements+=("kubectl (${min_kubectl_version} or newer, found ${kubectl_version})")
            fi
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
    deploy_cluster_issuer
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
    echo "  🐱 Installing Tekton Operator..." >&2
    # Operator
    kubectl apply -k "${script_path}/dependencies/tekton-operator"
    retry "kubectl wait --for=condition=Ready -l app=tekton-operator -n tekton-operator pod --timeout=240s" \
          "Tekton Operator did not become available within the allocated time"
    # Wait for the operator to create configs for the first time before applying configs
    retry "kubectl wait --for=condition=Ready tektonconfig/config --timeout=360s" \
          "Tekton Config resource was not created within the allocated time"
    echo "  ⚙️  Configuring Tekton..." >&2
    retry "kubectl apply -k ${script_path}/dependencies/tekton-config" \
          "The Tekton Config resource was not updated within the allocated time"

    echo "  🔄 Setting up Pipeline As Code..." >&2
    kubectl apply -k "${script_path}/dependencies/pipelines-as-code"

    # Wait for the operator to reconcile after applying the configs
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=60s

    echo "  🔐 Setting up Tekton Chains RBAC..." >&2
    kubectl apply -k "${script_path}/dependencies/tekton-chains-rbac"
}

deploy_cert_manager() {
    kubectl apply -k "${script_path}/dependencies/cert-manager"
    sleep 5
    retry "kubectl wait --for=condition=Available --timeout=120s deployment -l app.kubernetes.io/instance=cert-manager -n cert-manager" \
          "Cert manager did not become available within the allocated time"
}

deploy_trust_manager() {
    kubectl apply -k "${script_path}/dependencies/trust-manager"
    sleep 5
    retry "kubectl wait --for=condition=Ready --timeout=60s -l app.kubernetes.io/instance=trust-manager -n cert-manager pod" \
          "Trust manager did not become available within the allocated time"
}

deploy_cluster_issuer() {
    : "${SKIP_CLUSTER_ISSUER:=false}"
    if [[ "${SKIP_CLUSTER_ISSUER}" == "true" ]]; then
        echo "⏭️  Skipping Cluster Issuer deployment (managed by operator)" >&2
        return 0
    fi
    kubectl apply -k "${script_path}/dependencies/cluster-issuer"
}

deploy_dex() {
    : "${SKIP_DEX:=false}"
    if [[ "${SKIP_DEX}" == "true" ]]; then
        echo "⏭️  Skipping Dex deployment (managed by operator)" >&2
        return 0
    fi
    kubectl apply -k "${script_path}/dependencies/dex"
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
    : "${SKIP_INTERNAL_REGISTRY:=false}"
    if [[ "${SKIP_INTERNAL_REGISTRY}" == "true" ]]; then
        echo "⏭️  Skipping Internal Registry deployment (managed by operator)" >&2
        return 0
    fi
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

deploy_kyverno() {
    kubectl apply -k "${script_path}/dependencies/kyverno" --server-side
    # Wait for policy CRD to be installed. Don't need to wait for everything to be up
    sleep 5
    kubectl apply -k "${script_path}/dependencies/kyverno/policy"
}

deploy_konflux_info() {
    : "${SKIP_KONFLUX_INFO:=false}"
    if [[ "${SKIP_KONFLUX_INFO}" == "true" ]]; then
        echo "⏭️  Skipping Konflux Info deployment (managed by operator)" >&2
        return 0
    fi
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
