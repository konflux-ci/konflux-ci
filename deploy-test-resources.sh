#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Deploying test resources..." >&2
    deploy
}

deploy() {
    echo "Setting up demo users..." >&2
    kubectl apply --server-side --force-conflicts -k "${script_path}/test/resources/demo-users/user/"

    # Sample onboarding manifests under demo-users/user/sample-components/ (optional).
    # CI sets SKIP_SAMPLE_COMPONENTS=true; omit it for full local demo deploys.
    if [[ "${SKIP_SAMPLE_COMPONENTS:-}" != "true" ]]; then
        echo "Deploying sample components..." >&2
        kubectl apply --server-side --force-conflicts -k "${script_path}/test/resources/demo-users/user/sample-components/"
    else
        echo "Skipping sample components (SKIP_SAMPLE_COMPONENTS=true)" >&2
    fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
