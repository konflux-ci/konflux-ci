#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")" 

main() {
    echo "Deploying test resources" >&2
    deploy
}

deploy() {
    kubectl create -k "${script_path}/test/resources/demo-users/user/"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
