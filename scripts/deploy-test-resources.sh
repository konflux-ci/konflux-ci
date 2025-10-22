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
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
