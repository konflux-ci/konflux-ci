#!/bin/bash -e

main() {
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=60s
    kubectl wait --for=condition=Available deployment --all -A --timeout=60s
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
