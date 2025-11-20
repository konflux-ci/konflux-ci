#!/bin/bash -eu

# Setup QEMU emulation in Kind cluster for cross-architecture support
# This enables running AMD64 containers on ARM64 Macs

main() {
    local cluster_name="${1:-konflux}"

    echo "🔧 Setting up QEMU x86_64 emulation in Kind cluster..." >&2

    # Check if we're on ARM64 architecture
    local arch
    arch=$(uname -m)
    if [[ "$arch" != "arm64" && "$arch" != "aarch64" ]]; then
        echo "  ℹ️  Not on ARM64 architecture ($arch), skipping QEMU setup" >&2
        return 0
    fi

    # Get the control plane container name
    local container_name="${cluster_name}-control-plane"

    # Check if QEMU binaries are available (from extraMounts in kind-config.yaml)
    if ! docker exec "$container_name" test -f /usr/bin/qemu-x86_64-static 2>/dev/null && \
       ! podman exec "$container_name" test -f /usr/bin/qemu-x86_64-static 2>/dev/null; then
        echo "  ⚠️  QEMU binaries not found in Kind container" >&2
        echo "  ⚠️  Ensure kind-config.yaml has extraMounts for /usr/bin/qemu-*-static" >&2
        return 1
    fi

    # Determine which container runtime is being used
    local exec_cmd
    if command -v docker &> /dev/null && docker ps --filter "name=^${container_name}$" --format '{{.Names}}' | grep -q "^${container_name}$"; then
        exec_cmd="docker exec $container_name"
    elif command -v podman &> /dev/null && podman ps --filter "name=^${container_name}$" --format '{{.Names}}' | grep -q "^${container_name}$"; then
        exec_cmd="podman exec $container_name"
    else
        echo "  ❌ Could not find Kind container $container_name" >&2
        return 1
    fi

    # Mount binfmt_misc if not already mounted
    if ! $exec_cmd test -f /proc/sys/fs/binfmt_misc/status 2>/dev/null; then
        echo "  📁 Mounting binfmt_misc..." >&2
        $exec_cmd mount binfmt_misc -t binfmt_misc /proc/sys/fs/binfmt_misc
    fi

    # Check if qemu-x86_64 is already registered
    if $exec_cmd test -f /proc/sys/fs/binfmt_misc/qemu-x86_64 2>/dev/null; then
        echo "  ✅ QEMU x86_64 already registered" >&2
        return 0
    fi

    # Register QEMU x86_64 emulation
    echo "  📝 Registering QEMU x86_64 emulation..." >&2
    $exec_cmd sh -c 'echo ":qemu-x86_64:M::\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00\x3e\x00:\xff\xff\xff\xff\xff\xfe\xff\x00\xff\xff\xff\xff\xff\xff\xff\xff\xfe\xff\xff\xff:/usr/bin/qemu-x86_64-static:OCF" > /proc/sys/fs/binfmt_misc/register'

    # Verify registration
    if $exec_cmd test -f /proc/sys/fs/binfmt_misc/qemu-x86_64; then
        echo "  ✅ QEMU x86_64 emulation registered successfully" >&2
        $exec_cmd cat /proc/sys/fs/binfmt_misc/qemu-x86_64 >&2
    else
        echo "  ❌ Failed to register QEMU x86_64 emulation" >&2
        return 1
    fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
