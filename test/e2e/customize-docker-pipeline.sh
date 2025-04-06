#!/bin/bash
set -euo pipefail

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Apply Kyverno policy"
    kubectl apply -f  "${script_path}/../../dependencies/kyverno/policy/e2e-reduce-resources.yaml"
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
