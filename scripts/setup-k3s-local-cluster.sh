#!/usr/bin/env bash
# Setup a native k3s cluster for Konflux (Linux only).
#
# See docs/local-k3s.md for host firewall / sudo requirements.
#
# Env:
#   K3S_VERSION              - optional pin (e.g. v1.36.2+k3s1); default: channel stable
#   K3S_KUBECONFIG           - user kubeconfig path (default: ~/.kube/k3s.yaml)
#   K3S_CONFIGURE_FIREWALL   - if 1 (default) and firewall-cmd exists, apply runtime rules
#   K3S_FIREWALL_ZONE        - egress zone for masquerade (default: FedoraWorkstation, else public)
#   K3S_DISABLE_TRAFFIC_FW   - if 1, skip firewall configuration
#   K3S_PUBLISH_KIND_PORTS   - if 0, skip Kind hostPort→NodePort publish (default: publish)
#   REGISTRY_HOST_PORT       - host port for registry mapping (default: 5001; see kind-config.yaml)
#
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
	echo "ERROR: native k3s is only supported on Linux. On macOS use Kind (CLUSTER_BACKEND=kind)." >&2
	exit 1
fi

K3S_KUBECONFIG="${K3S_KUBECONFIG:-${HOME}/.kube/k3s.yaml}"
K3S_CONFIGURE_FIREWALL="${K3S_CONFIGURE_FIREWALL:-1}"
K3S_DISABLE_TRAFFIC_FW="${K3S_DISABLE_TRAFFIC_FW:-0}"

configure_firewalld() {
	if [[ "${K3S_DISABLE_TRAFFIC_FW}" == "1" || "${K3S_CONFIGURE_FIREWALL}" == "0" ]]; then
		echo "Skipping firewalld configuration (K3S_CONFIGURE_FIREWALL=${K3S_CONFIGURE_FIREWALL}, K3S_DISABLE_TRAFFIC_FW=${K3S_DISABLE_TRAFFIC_FW})"
		return 0
	fi
	if ! command -v firewall-cmd >/dev/null 2>&1; then
		return 0
	fi
	if ! firewall-cmd --state >/dev/null 2>&1; then
		echo "firewalld installed but not running; skipping firewall rules"
		return 0
	fi

	local zone="${K3S_FIREWALL_ZONE:-}"
	if [[ -z "${zone}" ]]; then
		if firewall-cmd --get-zones 2>/dev/null | tr ' ' '\n' | grep -qx 'FedoraWorkstation'; then
			zone=FedoraWorkstation
		else
			zone=public
		fi
	fi

	echo "Configuring firewalld (runtime) for k3s pod egress (zone=${zone})..."
	echo "  See docs/local-k3s.md — requires sudo"
	sudo firewall-cmd --add-port=6443/tcp
	sudo firewall-cmd --add-port=10250/tcp
	sudo firewall-cmd --add-port=8472/udp
	sudo firewall-cmd --add-port=51820/udp
	sudo firewall-cmd --zone=trusted --add-source=10.42.0.0/16
	sudo firewall-cmd --zone=trusted --add-source=10.43.0.0/16
	sudo firewall-cmd --zone="${zone}" --add-masquerade
	echo "✓ firewalld runtime rules applied"
}

install_or_reuse_k3s() {
	if systemctl is-active --quiet k3s 2>/dev/null; then
		echo "k3s service already active; reusing"
		return 0
	fi
	if command -v k3s >/dev/null 2>&1 && sudo k3s kubectl get nodes >/dev/null 2>&1; then
		echo "k3s binary present and API reachable; ensuring service is started"
		sudo systemctl start k3s || true
		return 0
	fi

	echo "Installing k3s (Traefik disabled)..."
	local install_args=(--write-kubeconfig-mode 644 --disable traefik)
	if [[ -n "${K3S_VERSION:-}" ]]; then
		echo "  Pinning INSTALL_K3S_VERSION=${K3S_VERSION}"
		curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="${K3S_VERSION}" sh -s - "${install_args[@]}"
	else
		curl -sfL https://get.k3s.io | sh -s - "${install_args[@]}"
	fi
}

write_kubeconfig() {
	mkdir -p "$(dirname "${K3S_KUBECONFIG}")"
	if [[ -r /etc/rancher/k3s/k3s.yaml ]]; then
		cp /etc/rancher/k3s/k3s.yaml "${K3S_KUBECONFIG}"
	else
		sudo cp /etc/rancher/k3s/k3s.yaml "${K3S_KUBECONFIG}"
		sudo chown "$(id -u):$(id -g)" "${K3S_KUBECONFIG}"
	fi
	chmod 600 "${K3S_KUBECONFIG}" 2>/dev/null || true
	export KUBECONFIG="${K3S_KUBECONFIG}"
	echo "KUBECONFIG=${KUBECONFIG}"

	# Propagate to GitHub Actions subsequent steps when present.
	if [[ -n "${GITHUB_ENV:-}" ]]; then
		echo "KUBECONFIG=${KUBECONFIG}" >>"${GITHUB_ENV}"
	fi
}

# Same shape as deploy-deps.sh / deploy-image-controller.sh: retry a command a few times.
retry() {
	local ret=0
	for i in {1..3}; do
		ret=0
		# shellcheck disable=SC2086
		$1 || ret="$?"
		if [[ "$ret" -eq 0 ]]; then
			return 0
		fi
		if [[ "$i" -lt 3 ]]; then
			echo "🔄 Retrying command (attempt $((i + 1))/3)..." >&2
			sleep 3
		fi
	done
	echo "$1": "$2." >&2
	return "$ret"
}

wait_for_node() {
	export KUBECONFIG="${K3S_KUBECONFIG}"

	# kubectl wait --all fails immediately with "no matching resources found" if the
	# API is up but the node object is not registered yet (common right after k3s start).
	# Match deploy-deps.sh: until the object exists, then retry the wait.
	echo "Waiting for k3s node object to appear..."
	local attempts=0
	until kubectl get nodes --no-headers 2>/dev/null | grep -q .; do
		attempts=$((attempts + 1))
		if ((attempts > 60)); then
			echo "Timed out waiting for k3s to register a node" >&2
			kubectl get nodes -o wide >&2 || true
			return 1
		fi
		sleep 2
	done

	echo "Waiting for k3s node Ready..."
	retry "kubectl wait --for=condition=Ready nodes --all --timeout=120s" \
		"k3s node did not become Ready within the allocated time"
	kubectl get nodes -o wide
	kubectl get storageclass
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "========================================="
echo "Konflux k3s cluster setup"
echo "========================================="

configure_firewalld
install_or_reuse_k3s
write_kubeconfig
wait_for_node

# Kind hostPorts are the public API (uiURL https://localhost:9443, etc.).
# Native k3s only binds NodePorts (30011, …); republish to match kind-config.yaml.
"${SCRIPT_DIR}/publish-kind-compatible-host-ports.sh"

echo "✓ k3s cluster ready"
