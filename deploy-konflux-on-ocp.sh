#!/bin/bash -e

main() {
    echo "Deploying Konflux" >&2
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
