#!/bin/bash -e

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Restarting Kind cluster" >&2
    podman restart konflux-control-plane
    echo "Updating PID limit" >&2
    podman update --pids-limit 4096 konflux-control-plane
    echo "Increasing resource limit" >&2
    sudo sysctl fs.inotify.max_user_watches=524288
    sudo sysctl fs.inotify.max_user_instances=512
    echo "Please wait for few minutes for the Konflux UI to be available." >&2
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
