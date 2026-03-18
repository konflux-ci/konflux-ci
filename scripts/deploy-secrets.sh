#!/usr/bin/env bash
#
# Setup Konflux Secrets
#
# Creates the required secrets for a Konflux deployment.
#
# Prerequisites:
#   - kubectl configured with cluster access
#
# Required environment variables:
#   GITHUB_APP_ID            - GitHub App ID for Pipelines-as-Code
#   WEBHOOK_SECRET           - GitHub webhook secret
#   GITHUB_PRIVATE_KEY       - GitHub App private key (literal content)
#     OR
#   GITHUB_PRIVATE_KEY_PATH  - Path to GitHub App private key .pem file
#
# Optional environment variables:
#   QUAY_TOKEN               - Quay.io push token (for image-controller)
#   QUAY_ORGANIZATION        - Quay.io organization (for image-controller)
#   USE_OPENSHIFT_PIPELINES  - Set to "true" to indicate that OpenShift Pipelines is being used
#                              instead of upstream Tekton (default: false)
#   CREATE_NAMESPACES        - Set to "true" to create namespaces instead of waiting
#                              for the operator to create them (for OPERATOR_INSTALL_METHOD=none)
#   WAIT_FOR_PODS            - Set to "false" to skip waiting for pods to become ready
#                              (default: true)

set -euo pipefail

validate_inputs() {
    GITHUB_APP_ID="${GITHUB_APP_ID:?GITHUB_APP_ID is required}"
    WEBHOOK_SECRET="${WEBHOOK_SECRET:?WEBHOOK_SECRET is required}"

    if [ -z "${GITHUB_PRIVATE_KEY:-}" ] && [ -z "${GITHUB_PRIVATE_KEY_PATH:-}" ]; then
        echo "ERROR: GitHub private key is required" >&2
        echo "Set GITHUB_PRIVATE_KEY or GITHUB_PRIVATE_KEY_PATH" >&2
        exit 1
    fi

    if [ -n "${GITHUB_PRIVATE_KEY_PATH:-}" ] && [ ! -f "${GITHUB_PRIVATE_KEY_PATH}" ]; then
        echo "ERROR: GitHub private key file not found: ${GITHUB_PRIVATE_KEY_PATH}" >&2
        exit 1
    fi
}

main() {
    validate_inputs

    if [ "${VALIDATE_ONLY:-}" = "true" ]; then
        return
    fi

    echo "Deploying secrets..."
    create_github_integration_secrets
    create_image_controller_secret
}

# Ensure a namespace exists. In CREATE_NAMESPACES mode, create it immediately.
# Otherwise, wait up to 60s for the operator to create it.
ensure_namespace() {
    local ns="$1"
    if [ "${CREATE_NAMESPACES:-}" = "true" ]; then
        echo "Creating namespace: ${ns}"
        kubectl create namespace "${ns}" --dry-run=client -o yaml | kubectl apply -f -
    else
        echo "Waiting for namespace: ${ns}"
        local timeout=60
        while ! kubectl get namespace "${ns}" &> /dev/null && [ $timeout -gt 0 ]; do
            sleep 2
            timeout=$((timeout - 2))
        done
        if [ $timeout -le 0 ]; then
            echo "WARNING: Namespace ${ns} not found after 60 seconds"
            return 1
        fi
    fi
}

create_github_integration_secrets() {
    echo "=== Creating GitHub integration secrets ==="

    local pac_ns="pipelines-as-code"
    if [ "${USE_OPENSHIFT_PIPELINES:-false}" = "true" ]; then
        pac_ns="openshift-pipelines"
    fi

    for ns in "${pac_ns}" build-service integration-service; do
        if ! ensure_namespace "${ns}"; then
            echo "         Secret will need to be created manually in ${ns}"
            continue
        fi

        echo "Creating secret in ${ns}..."
        if [ -n "${GITHUB_PRIVATE_KEY_PATH:-}" ] && [ -f "${GITHUB_PRIVATE_KEY_PATH}" ]; then
            kubectl -n "$ns" create secret generic pipelines-as-code-secret \
                --from-file=github-private-key="${GITHUB_PRIVATE_KEY_PATH}" \
                --from-literal github-application-id="$GITHUB_APP_ID" \
                --from-literal webhook.secret="$WEBHOOK_SECRET" \
                --dry-run=client -o yaml | kubectl apply -f -
        else
            kubectl -n "$ns" create secret generic pipelines-as-code-secret \
                --from-literal github-private-key="$GITHUB_PRIVATE_KEY" \
                --from-literal github-application-id="$GITHUB_APP_ID" \
                --from-literal webhook.secret="$WEBHOOK_SECRET" \
                --dry-run=client -o yaml | kubectl apply -f -
        fi
    done

    echo "GitHub integration secrets created."
}

create_image_controller_secret() {
    if [ -n "${QUAY_TOKEN:-}" ] && [ -n "${QUAY_ORGANIZATION:-}" ]; then
        echo ""
        echo "=== Creating image-controller Quay secret ==="

        if ! ensure_namespace "image-controller"; then
            echo "         Secret will need to be created manually"
            return
        fi

        local quay_secret_args=(
            --from-literal=quaytoken="${QUAY_TOKEN}"
            --from-literal=organization="${QUAY_ORGANIZATION}"
        )
        if [ -n "${QUAY_API_URL:-}" ]; then
            quay_secret_args+=(--from-literal=quayapiurl="${QUAY_API_URL}")
            echo "Using custom Quay API URL: ${QUAY_API_URL}"
        fi
        kubectl -n image-controller create secret generic quaytoken \
            "${quay_secret_args[@]}" \
            --dry-run=client -o yaml | kubectl apply -f -
        echo "Image-controller secret created."

        if [ "${WAIT_FOR_PODS:-true}" = "true" ]; then
            echo "Waiting for image-controller pods..."
            if kubectl wait --for=condition=Ready --timeout=240s \
                -l control-plane=controller-manager -n image-controller pod 2>/dev/null; then
                echo "Image-controller is ready."
            else
                echo "WARNING: Image-controller pods did not become ready within 4 minutes"
            fi
        fi
    elif [ -n "${QUAY_TOKEN:-}" ] || [ -n "${QUAY_ORGANIZATION:-}" ]; then
        echo ""
        echo "WARNING: Both QUAY_TOKEN and QUAY_ORGANIZATION must be set for image-controller"
    fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
