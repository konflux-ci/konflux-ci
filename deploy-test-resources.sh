#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")" 

main() {
    echo "🧪 Deploying test resources..." >&2
    deploy
}

deploy() {
    echo "👥 Setting up demo users..." >&2
    kubectl apply -k "${script_path}/test/resources/demo-users/user/"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
