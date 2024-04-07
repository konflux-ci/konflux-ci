#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")" 

main() {
    echo "Deploying Konflux" >&2
    install_konflux

    echo "Waiting for Konflux to be ready" >&2
    "${script_path}/wait-for-all-.sh"
}

deploy() {
    kubectl "${script_path}/test/resources/demo-users"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
