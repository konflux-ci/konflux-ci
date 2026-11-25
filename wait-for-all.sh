#!/bin/bash -e

main() {
    echo "⏳ Waiting for Tekton configuration to be ready..." >&2
    kubectl wait --for=condition=Ready tektonconfig/config --timeout=120s
    # profile: basic does not keep Tekton Dashboard; exclude it so deployment --all
    # does not race on create/delete NotFound while the operator tears it down.
    echo "⏳ Waiting for all deployments to be available..." >&2
    kubectl wait --for=condition=Available deployment --all -A \
        --field-selector metadata.name!=tekton-dashboard \
        --timeout=240s
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
