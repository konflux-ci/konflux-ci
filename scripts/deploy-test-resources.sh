#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"
repo_root="$(dirname -- "${script_path}")"

main() {
    echo "🧪 Deploying test resources..." >&2
    deploy
}

deploy() {
    echo "👥 Setting up demo users..." >&2
    kubectl apply -k "${repo_root}/test/resources/demo-users/user/"

    echo "🔐 Configuring Dex with demo credentials..." >&2
    kubectl apply -f "${repo_root}/test/resources/demo-users/dex-users.yaml"

    echo "🔧 Patching Dex deployment to use demo configmap..." >&2
    kubectl patch deployment dex -n dex --type=json -p='[{"op": "replace", "path": "/spec/template/spec/volumes/0/configMap/name", "value": "dex"}]'

    echo "⏳ Waiting for Dex to restart with demo users..." >&2
    kubectl rollout status deployment/dex -n dex --timeout=120s
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
