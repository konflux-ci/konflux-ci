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

# get installed version of a tool
# each tool has its own way of getting the version
get_installed_version() {
    local tool_name="$1"
    local version=""
    if ! command -v "$tool_name" &> /dev/null; then
        echo "NOT_INSTALLED"
        return
    fi

    case "$tool_name" in
        kubectl)
            # Method 1: Try JSON output
            version=$(kubectl version --client -o json 2>/dev/null | grep '"gitVersion"' 2>/dev/null | sed 's/.*"gitVersion": "\([^"]*\)".*/\1/' 2>/dev/null | sed 's/v//' 2>/dev/null)

            # Method 2: Try client version output
            if [ -z "$version" ]; then
                version=$(kubectl version --client 2>/dev/null | grep 'Client Version:' 2>/dev/null | awk '{print $3}' 2>/dev/null | sed 's/^v//' 2>/dev/null)
            fi
            ;;
        openssl)
            version=$(openssl version 2>/dev/null | awk '{print $2}' 2>/dev/null)
            ;;
        *)
            echo "UNKNOWN_TOOL"
            return
            ;;
    esac

    if [ -z "$version" ]; then
        echo "PARSE_ERROR"
    else
        # Remove any non-numeric suffix (e.g., "3.2.2-beta" -> "3.2.2")
        version=${version%%[^0-9.]*}
        echo "$version"
    fi
}

# Checks if an actual, installed version is sufficient to meet a minimum requirement.
# Returns 0 (true) if actual_version >= min_required_version
# Returns 1 (false) if actual_version < min_required_version
#
is_version_sufficient () {
    local actual_version=$1
    local min_required_version=$2

    # Split version strings into arrays based on the '.' delimiter
    local OLD_IFS="$IFS"
    IFS='.'
    local actual_parts=()
    local min_parts=()
    read -ra actual_parts <<< "$actual_version"
    read -ra min_parts <<< "$min_required_version"
    IFS="$OLD_IFS"

    # Get the length of the longest version string
    local actual_len=${#actual_parts[@]}
    local min_len=${#min_parts[@]}
    local max_len=$((actual_len > min_len ? actual_len : min_len))

    # Loop through each part (major, minor, patch, etc.)
    for ((i=0; i<max_len; i++)); do
        # Get the numeric value for each part, defaulting to 0 if it doesn't exist
        # (e.g., comparing "1.2" with "1.2.1")
        local actual_part=${actual_parts[i]:-0}
        local min_part=${min_parts[i]:-0}

        # Force base-10 to prevent errors with leading zeros (e.g., "08")
        local actual_num=$((10#$actual_part))
        local min_num=$((10#$min_part))

        if ((actual_num > min_num)); then
            return 0 # Sufficient (e.g., 2 > 1)
        fi
        if ((actual_num < min_num)); then
            return 1 # Insufficient (e.g., 1 < 2)
        fi
        # If parts are equal (e.g., 1 == 1), continue to the next part
    done

    # If the loop finishes, the versions are identical, which is sufficient.
    return 0
}

check_req(){
    local REQUIREMENTS=(
        "kubectl:1.31.1"
        "openssl:3.0.13"
    )

    local all_ok=true
    local error_messages=()

    echo "Checking system requirements..."

    # Loop through each "tool:version" pair in the array
    for req in "${REQUIREMENTS[@]}"; do
        # Split the string on the colon ":"
        local tool="${req%%:*}"
        local min_ver="${req##*:}"

        echo -n "  Checking $tool... "

        local actual_ver
        actual_ver=$(get_installed_version "$tool")
        echo "actual_ver: $actual_ver"
        echo "min_ver: $min_ver"
        # Handle cases where getting the version failed
        if [[ "$actual_ver" == "NOT_INSTALLED" ]]; then
            echo "FAIL"
            error_messages+=("  - $tool: NOT installed (required >= $min_ver)")
            all_ok=false
            continue
        fi
        if [[ "$actual_ver" == "PARSE_ERROR" ]]; then
            echo "FAIL"
            error_messages+=("  - $tool: Could not parse installed version.")
            all_ok=false
            continue
        fi
        if [[ "$actual_ver" == "UNKNOWN_TOOL" ]]; then
            echo "FAIL"
            error_messages+=("  - $tool: Version extraction not implemented for this tool.")
            all_ok=false
            continue
        fi
        if is_version_sufficient "$actual_ver" "$min_ver"; then
            # Success: The function returned 0 (true)
            echo "OK (Installed: $actual_ver, Required: >= $min_ver)"
        else
            # Failure: The function returned 1 (false)
            echo "FAIL"
            error_messages+=("  - $tool: Version too old (Installed: $actual_ver, Required: >= $min_ver)")
            all_ok=false
        fi
    done

    if [ "$all_ok" = false ]; then
        echo
        echo "❌ Error: One or more system requirements are not met."
        printf "  %s\n" "${error_messages[@]}"
        echo
        exit 1
    else
        echo
        echo "✅ All system requirements are met. Continuing..."
    fi
}

deploy() {
    echo "🔐 Deploying Cert Manager..." >&2
    deploy_cert_manager
    echo "🤝 Deploying Trust Manager..." >&2
    deploy_trust_manager
    echo "📜 Setting up Cluster Issuer..." >&2
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
        echo "🔑 Creating secret oauth2-proxy-client-secret" >&2
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

deploy_kyverno() {
    kubectl apply -k "${script_path}/dependencies/kyverno" --server-side
    # Wait for policy CRD to be installed. Don't need to wait for everything to be up
    sleep 5
    kubectl apply -k "${script_path}/dependencies/kyverno/policy"
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
