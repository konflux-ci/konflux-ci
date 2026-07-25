#!/usr/bin/env bash
# Publish Kind-compatible host ports onto native NodePorts (k3s / bare metal).
#
# Kind's extraPortMappings (kind-config.yaml) make hostPort the public API while
# Services use NodePorts in the 30000+ range. Native k3s exposes only the
# NodePort numbers on the host, so clients that dial https://localhost:9443
# (status.uiURL, Dex/oauth2-proxy) get connection refused unless we republish.
#
# This script mirrors kind-config.yaml hostPort → containerPort (NodePort):
#   8888 → 30010
#   9443 → 30011  (UI HTTPS)
#   8180 → 30012  (PaC)
#   5001 → 30001  (registry; override with REGISTRY_HOST_PORT)
#   8443 → 30002  (Quay)
#
# Env:
#   REGISTRY_HOST_PORT           - host port for registry (default: 5001)
#   K3S_PUBLISH_KIND_PORTS       - set to 0 to skip (default: publish)
#   KONFLUX_KIND_PORT_STATE_DIR  - pid/log dir (default: under XDG_RUNTIME_DIR or /tmp)
#
set -euo pipefail

if [[ "${K3S_PUBLISH_KIND_PORTS:-1}" == "0" ]]; then
	echo "Skipping Kind-compatible host port publish (K3S_PUBLISH_KIND_PORTS=0)"
	exit 0
fi

REGISTRY_HOST_PORT="${REGISTRY_HOST_PORT:-5001}"
STATE_DIR="${KONFLUX_KIND_PORT_STATE_DIR:-${XDG_RUNTIME_DIR:-/tmp}/konflux-kind-compatible-ports}"
mkdir -p "${STATE_DIR}"

# host_port:node_port — keep in sync with kind-config.yaml extraPortMappings
PORT_MAPPINGS=(
	"8888:30010"
	"9443:30011"
	"8180:30012"
	"${REGISTRY_HOST_PORT}:30001"
	"8443:30002"
)

ensure_socat() {
	if command -v socat >/dev/null 2>&1; then
		return 0
	fi
	echo "socat not found; installing (required to publish Kind-compatible host ports)..."
	if command -v apt-get >/dev/null 2>&1; then
		sudo apt-get update -qq
		sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq socat
	elif command -v dnf >/dev/null 2>&1; then
		sudo dnf install -y socat
	elif command -v yum >/dev/null 2>&1; then
		sudo yum install -y socat
	else
		echo "ERROR: install socat (used to map Kind host ports → NodePorts), then re-run." >&2
		exit 1
	fi
	command -v socat >/dev/null 2>&1
}

# True if our socat for this host port is still running.
our_listener_alive() {
	local host_port=$1
	local pidfile="${STATE_DIR}/tcp4-${host_port}.pid"
	local pid
	[[ -f "${pidfile}" ]] || return 1
	pid="$(cat "${pidfile}")"
	kill -0 "${pid}" 2>/dev/null
}

# True if anything is listening on the host port (IPv4 or IPv6).
host_port_busy() {
	local host_port=$1
	ss -ltnH "sport = :${host_port}" 2>/dev/null | grep -q .
}

stop_our_listeners() {
	local host_port=$1
	local fam pidfile pid
	for fam in tcp4 tcp6; do
		pidfile="${STATE_DIR}/${fam}-${host_port}.pid"
		if [[ -f "${pidfile}" ]]; then
			pid="$(cat "${pidfile}")"
			kill "${pid}" 2>/dev/null || true
			rm -f "${pidfile}"
		fi
	done
}

start_listener() {
	local fam=$1 host_port=$2 node_port=$3
	local pidfile="${STATE_DIR}/${fam}-${host_port}.pid"
	local logfile="${STATE_DIR}/${fam}-${host_port}.log"
	local listen_addr

	case "${fam}" in
	tcp4)
		listen_addr="TCP4-LISTEN:${host_port},fork,reuseaddr,bind=0.0.0.0"
		;;
	tcp6)
		# ipv6only=1 so this does not steal IPv4; forward to IPv4 NodePort.
		listen_addr="TCP6-LISTEN:${host_port},fork,reuseaddr,bind=[::],ipv6only=1"
		;;
	*)
		echo "ERROR: unknown address family ${fam}" >&2
		return 1
		;;
	esac

	# NodePorts listen on the host; prefer IPv4 loopback as the upstream.
	nohup socat "${listen_addr}" "TCP:127.0.0.1:${node_port}" >"${logfile}" 2>&1 &
	echo $! >"${pidfile}"
	sleep 0.1
	if ! kill -0 "$(cat "${pidfile}")" 2>/dev/null; then
		echo "ERROR: socat ${fam} :${host_port} → :${node_port} failed to start; log:" >&2
		cat "${logfile}" >&2 || true
		rm -f "${pidfile}"
		return 1
	fi
}

publish_one() {
	local host_port=$1 node_port=$2

	if our_listener_alive "${host_port}"; then
		echo "  :${host_port} → :${node_port} (already published)"
		return 0
	fi

	if host_port_busy "${host_port}"; then
		echo "ERROR: host port ${host_port} is already in use (and not by this script)." >&2
		echo "  Free it, or stop Kind if both stacks share Kind host ports." >&2
		echo "  To skip publishing: K3S_PUBLISH_KIND_PORTS=0" >&2
		ss -ltnH "sport = :${host_port}" >&2 || true
		return 1
	fi

	stop_our_listeners "${host_port}"
	start_listener tcp4 "${host_port}" "${node_port}"
	# IPv6 localhost ([::1]) is what Go often dials for "localhost"; Kind hostPorts accept it.
	if [[ -e /proc/net/if_inet6 ]]; then
		start_listener tcp6 "${host_port}" "${node_port}" || {
			echo "WARNING: IPv6 publish for :${host_port} failed; IPv4-only (127.0.0.1:${host_port} still works)" >&2
		}
	fi
	echo "  :${host_port} → :${node_port}"
}

ensure_socat

echo "Publishing Kind-compatible host ports → NodePorts..."
for mapping in "${PORT_MAPPINGS[@]}"; do
	publish_one "${mapping%%:*}" "${mapping##*:}"
done
echo "✓ Kind-compatible host ports published (state: ${STATE_DIR})"
