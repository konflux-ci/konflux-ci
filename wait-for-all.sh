#!/bin/bash -e

main() {
    echo "⏳ Waiting for Tekton configuration to be ready..." >&2
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=120s
    echo "⏳ Waiting for all deployments to be available..." >&2
    kubectl wait --for=condition=Available deployment --all -A --timeout=240s
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
