#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Deploying test resources..." >&2
    deploy
}

# Wait for Kyverno to finish processing all generate rules (e.g., creating
# ServiceAccounts and RoleBindings in tenant namespaces). UpdateRequests are
# created by Kyverno's admission controller when a resource matches a generate
# policy and are removed once the background controller completes processing.
wait_for_kyverno() {
    local timeout="${1:-60}"
    local elapsed=0

    echo "Waiting for Kyverno to finish processing generate rules..." >&2
    while [ "$elapsed" -lt "$timeout" ]; do
        local count
        count=$(kubectl get updaterequests.kyverno.io -A --no-headers | wc -l | tr -d ' ')
        if [ "$count" -eq 0 ]; then
            echo "Kyverno processing complete." >&2
            return 0
        fi
        echo "  ${count} UpdateRequest(s) pending (${elapsed}s/${timeout}s)..." >&2
        sleep 2
        elapsed=$((elapsed + 2))
    done

    echo "WARNING: Timed out waiting for Kyverno UpdateRequests after ${timeout}s" >&2
    kubectl get updaterequests.kyverno.io -A -o wide >&2
    return 1
}

deploy() {
    echo "Setting up demo users..." >&2
    kubectl apply --server-side --force-conflicts -k "${script_path}/test/resources/demo-users/user/"
    wait_for_kyverno

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
