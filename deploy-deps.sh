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
    echo "🐳 Deploying Quay..." >&2
    deploy_quay
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
    : "${USE_OPENSHIFT_PIPELINES:=false}"
    if [[ "${USE_OPENSHIFT_PIPELINES}" == "true" ]]; then
        echo "  🐱 Installing Tekton via OpenShift Pipelines Operator..." >&2
        deploy_openshift_pipelines
    else
        echo "  🐱 Installing Tekton via upstream Operator..." >&2
        deploy_upstream_tekton
    fi
}

deploy_openshift_pipelines() {
    # Install OpenShift Pipelines Operator via OLM
    kubectl apply -k "${script_path}/dependencies/openshift-pipelines-subscription"

    # Wait for TektonConfig CRD to be available (timeout: 10 minutes)
    echo "  ⏳ Waiting for TektonConfig CRD..." >&2
    local crd_timeout=600
    local crd_waited=0
    until kubectl get crd tektonconfigs.operator.tekton.dev &>/dev/null; do
        if [[ $crd_waited -ge $crd_timeout ]]; then
            echo "ERROR: TektonConfig CRD not available after ${crd_timeout}s" >&2
            exit 1
        fi
        sleep 10
        crd_waited=$((crd_waited + 10))
    done

    # Wait for TektonConfig resource to be created (timeout: 10 minutes)
    echo "  ⏳ Waiting for TektonConfig to be ready..." >&2
    local config_timeout=600
    local config_waited=0
    until kubectl get tektonconfig config &>/dev/null; do
        if [[ $config_waited -ge $config_timeout ]]; then
            echo "ERROR: TektonConfig resource not created after ${config_timeout}s" >&2
            exit 1
        fi
        sleep 10
        config_waited=$((config_waited + 10))
    done
    retry "kubectl wait --for=condition=Ready tektonconfig/config --timeout=600s" \
          "TektonConfig did not become ready within the allocated time"

    # Wait for Tekton webhook services (timeout: 5 minutes)
    echo "  ⏳ Waiting for Tekton webhook services..." >&2
    local webhook_timeout=300
    local webhook_waited=0
    until kubectl get service tekton-operator-proxy-webhook -n openshift-pipelines &>/dev/null; do
        if [[ $webhook_waited -ge $webhook_timeout ]]; then
            echo "ERROR: Tekton webhook service not available after ${webhook_timeout}s" >&2
            exit 1
        fi
        sleep 10
        webhook_waited=$((webhook_waited + 10))
    done
    retry "kubectl wait --for=condition=Available deployment/tekton-operator-proxy-webhook -n openshift-pipelines --timeout=120s" \
          "Tekton webhook did not become available within the allocated time"

    # Apply Tekton Chains RBAC for OpenShift Pipelines (uses openshift-pipelines namespace)
    echo "  🔐 Setting up Tekton Chains RBAC..." >&2
    kubectl apply -k "${script_path}/dependencies/tekton-chains-rbac-ocp"

    echo "  ✅ OpenShift Pipelines is ready!" >&2
}

deploy_upstream_tekton() {
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
    : "${USE_OPENSHIFT_CERTMANAGER:=false}"
    if [[ "${USE_OPENSHIFT_CERTMANAGER}" == "true" ]]; then
        deploy_openshift_certmanager
    else
        deploy_upstream_certmanager
    fi
}

deploy_openshift_certmanager() {
    echo "  🔐 Installing cert-manager via Red Hat Operator..." >&2
    # Install cert-manager Operator via OLM (requires Namespace + OperatorGroup + Subscription)
    kubectl apply -k "${script_path}/dependencies/cert-manager-subscription"

    # Wait for cert-manager CRDs to be available (timeout: 10 minutes)
    echo "  ⏳ Waiting for cert-manager CRDs..." >&2
    local crd_timeout=600
    local crd_waited=0
    until kubectl get crd certificates.cert-manager.io &>/dev/null; do
        if [[ $crd_waited -ge $crd_timeout ]]; then
            echo "ERROR: cert-manager CRDs not available after ${crd_timeout}s" >&2
            exit 1
        fi
        sleep 10
        crd_waited=$((crd_waited + 10))
    done

    # Wait for cert-manager deployments to be ready (timeout: 10 minutes)
    echo "  ⏳ Waiting for cert-manager to be ready..." >&2
    local deploy_timeout=600
    local deploy_waited=0
    until kubectl get deployment cert-manager -n cert-manager &>/dev/null; do
        if [[ $deploy_waited -ge $deploy_timeout ]]; then
            echo "ERROR: cert-manager deployment not created after ${deploy_timeout}s" >&2
            exit 1
        fi
        sleep 10
        deploy_waited=$((deploy_waited + 10))
    done
    retry "kubectl wait --for=condition=Available --timeout=300s deployment -l app.kubernetes.io/instance=cert-manager -n cert-manager" \
          "cert-manager did not become available within the allocated time"

    # Wait for CA bundle to be injected into webhook configuration
    # The x509 errors occur when the API server doesn't have the CA bundle to verify the webhook's certificate.
    # cert-manager's cainjector component populates this field once the webhook certificate is ready.
    echo "  ⏳ Waiting for cert-manager webhook CA bundle..." >&2
    local webhook_timeout=120
    local webhook_waited=0
    until kubectl get validatingwebhookconfiguration cert-manager-webhook \
        -o jsonpath='{.webhooks[0].clientConfig.caBundle}' 2>/dev/null | grep -q .; do
        if [[ $webhook_waited -ge $webhook_timeout ]]; then
            echo "ERROR: cert-manager webhook CA bundle not ready after ${webhook_timeout}s" >&2
            exit 1
        fi
        sleep 5
        webhook_waited=$((webhook_waited + 5))
    done

    # Wait for webhook service to have endpoints
    # The "no endpoints available" error occurs when the webhook pods aren't registered yet.
    echo "  ⏳ Waiting for cert-manager webhook endpoints..." >&2
    webhook_waited=0
    until kubectl get endpoints cert-manager-webhook -n cert-manager \
        -o jsonpath='{.subsets[0].addresses}' 2>/dev/null | grep -q .; do
        if [[ $webhook_waited -ge $webhook_timeout ]]; then
            echo "ERROR: cert-manager webhook endpoints not ready after ${webhook_timeout}s" >&2
            exit 1
        fi
        sleep 5
        webhook_waited=$((webhook_waited + 5))
    done

    echo "  ✅ Red Hat cert-manager Operator is ready!" >&2
}

deploy_upstream_certmanager() {
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
    : "${SKIP_SMEE:=false}"
    if [[ "${SKIP_SMEE}" == "true" ]]; then
        echo "⏭️  Skipping Smee deployment (not needed for OCP CI)" >&2
        return 0
    fi
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

    if [[ "${USE_OPENSHIFT_PIPELINES:-false}" == "true" ]]; then
        echo "  🔧 Patching smee-client for OpenShift Pipelines namespace..." >&2
        kubectl set env deployment/gosmee-client -n smee-client -c health-check-sidecar \
            DOWNSTREAM_SERVICE_URL="http://pipelines-as-code-controller.openshift-pipelines.svc.cluster.local:8080"
    fi
}

deploy_kyverno() {
    kubectl apply -k "${script_path}/dependencies/kyverno" --server-side
    # Policies are validated/mutated by the Kyverno admission webhook. The webhook is served
    # by kyverno-admission-controller; if we apply policies before that deployment is ready,
    # the API server gets "connection refused" from the webhook. Wait for it to be Available.
    echo "  ⏳ Waiting for Kyverno admission controller..." >&2
    retry "kubectl wait --for=condition=Available deployment/kyverno-admission-controller -n kyverno --timeout=30s" \
          "Kyverno admission controller did not become available within the allocated time"
    retry "kubectl apply -k ${script_path}/dependencies/kyverno/policy" \
          "Failed to apply Kyverno policies (webhook may not be ready yet)"
}

deploy_konflux_info() {
    : "${SKIP_KONFLUX_INFO:=false}"
    if [[ "${SKIP_KONFLUX_INFO}" == "true" ]]; then
        echo "⏭️  Skipping Konflux Info deployment (managed by operator)" >&2
        return 0
    fi
    kubectl apply -k "${script_path}/dependencies/konflux-info"
}

deploy_quay() {
    : "${SKIP_QUAY:=true}"
    if [[ "${SKIP_QUAY}" == "true" ]]; then
        echo "⏭️  Skipping Quay deployment" >&2
        return 0
    fi
    kubectl apply -k "${script_path}/dependencies/quay"

    local POSTGRES_PASSWORD
    if ! kubectl get secret quay-postgres-creds -n quay &>/dev/null; then
        echo "🔑 Creating quay-postgres-creds secret" >&2
        POSTGRES_PASSWORD="$(openssl rand -base64 20 | tr '+/' '-_' | tr -d '\n' | tr -d '=')"
        kubectl create secret generic quay-postgres-creds \
            --namespace=quay \
            --from-literal=password="$POSTGRES_PASSWORD"
    else
        POSTGRES_PASSWORD="$(kubectl get secret quay-postgres-creds -n quay \
            -o jsonpath='{.data.password}' | base64 -d)"
    fi

    if ! kubectl get secret quay-config -n quay &>/dev/null; then
        echo "🔑 Creating quay-config secret" >&2
        local DATABASE_SECRET_KEY SECRET_KEY config
        DATABASE_SECRET_KEY="$(openssl rand -hex 16)"
        SECRET_KEY="$(openssl rand -hex 16)"
        config="$(sed -e "s|\${POSTGRES_PASSWORD}|${POSTGRES_PASSWORD}|g" \
                      -e "s|\${DATABASE_SECRET_KEY}|${DATABASE_SECRET_KEY}|g" \
                      -e "s|\${SECRET_KEY}|${SECRET_KEY}|g" \
                      "${script_path}/dependencies/quay/quay-config.yaml.tpl")"
        kubectl create secret generic quay-config \
            --namespace=quay \
            --from-literal=config.yaml="$config"
    fi

    retry "kubectl wait --for=condition=Ready --timeout=240s -n quay -l app=quay-postgres pod" \
          "Quay Postgres did not become available within the allocated time"
    retry "kubectl wait --for=condition=Ready --timeout=240s -n quay -l app=quay-redis pod" \
          "Quay Redis did not become available within the allocated time"
    retry "kubectl wait --for=condition=Ready --timeout=240s -n quay -l app=quay pod" \
          "Quay did not become available within the allocated time"

    init_quay_admin
}

init_quay_admin() {
    : "${SKIP_QUAY_ADMIN_INIT:=false}"
    if [[ "${SKIP_QUAY_ADMIN_INIT}" == "true" ]]; then
        echo "⏭️  Skipping Quay admin initialization" >&2
        return 0
    fi

    for cmd in curl jq; do
        if ! command -v "$cmd" &>/dev/null; then
            echo "❌ '$cmd' is required for Quay admin initialization but not found" >&2
            return 1
        fi
    done

    if kubectl get secret quay-admin-token -n quay &>/dev/null; then
        echo "✅ Quay admin already initialized (quay-admin-token secret exists)" >&2
        return 0
    fi

    echo "👤 Initializing Quay admin user..." >&2

    local ADMIN_USER="quayadmin"
    local ADMIN_PASSWORD="password"  # gitleaks:allow -- throwaway ephemeral Kind cluster credential
    local ADMIN_EMAIL="admin@local.dev"

    local QUAY_URL="https://localhost:8443"

    echo "⏳ Waiting for Quay API to be reachable from host..." >&2
    local i
    for i in $(seq 1 30); do
        if curl -4 -sk --connect-timeout 2 "${QUAY_URL}/health/instance" 2>/dev/null \
            | grep -q '"status_code":200'; then
            echo "✅ Quay API is reachable" >&2
            break
        fi
        if [[ "$i" -eq 30 ]]; then
            echo "❌ Quay API not reachable from host within 60s" >&2
            return 1
        fi
        sleep 2
    done

    echo "📡 Creating admin user '${ADMIN_USER}'..." >&2
    local INIT_RESPONSE
    INIT_RESPONSE=$(curl -4 -sk --connect-timeout 10 -X POST "${QUAY_URL}/api/v1/user/initialize" \
        -H "Content-Type: application/json" \
        -d "{
            \"username\": \"${ADMIN_USER}\",
            \"password\": \"${ADMIN_PASSWORD}\",
            \"email\": \"${ADMIN_EMAIL}\",
            \"access_token\": true
        }")

    local TOKEN
    if echo "$INIT_RESPONSE" | jq -e '.access_token' &>/dev/null; then
        TOKEN=$(echo "$INIT_RESPONSE" | jq -r '.access_token')
        echo "✅ Admin user '${ADMIN_USER}' created" >&2
    else
        echo "❌ Failed to create admin: $(echo "$INIT_RESPONSE" | jq -r '.message // "unknown"')" >&2
        echo "❌ Response: ${INIT_RESPONSE}" >&2
        return 1
    fi

    kubectl create secret generic quay-admin-token --namespace=quay \
        --from-literal=token="${TOKEN}" \
        --from-literal=username="${ADMIN_USER}" \
        --from-literal=password="${ADMIN_PASSWORD}"
    echo "🔑 Admin token stored in secret 'quay-admin-token' in namespace 'quay'" >&2
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
