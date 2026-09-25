#!/usr/bin/env bash
# Obtain proxy auth tokens (Dex or OpenShift OAuth) and run proxy integration tests.
#
# Usage: run-proxy-integration-tests.sh [REPO_ROOT]
#
# Auth selection:
#   KONFLUX_PROXY_AUTH=openshift|dex (default: infer from the cluster platform)
#   openshift → OAuth authorization-code flow in test BeforeSuite (kubeadmin password required)
#   dex       → proxy tests use Dex password grant at runtime
#
# The OpenShift Konflux CR has no Dex static passwords, so the Dex password grant has
# no users behind it there. Auth mode is therefore selected from the platform, not from
# whether kubeadmin credentials happen to be present, and a missing OpenShift password
# is a hard error rather than a silent fallback that fails later with a token error.
set -euo pipefail

REPO_ROOT="$(cd "${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}" && pwd)"

# is_openshift returns success when the target cluster exposes OpenShift-only APIs.
# Falls back to oc when kubectl is absent. If neither client can reach a cluster the
# probe returns failure, which keeps the previous Dex default for local runs.
is_openshift() {
	local cli
	for cli in kubectl oc; do
		command -v "${cli}" &>/dev/null || continue
		if "${cli}" api-resources --api-group=route.openshift.io -o name 2>/dev/null |
			grep -q 'routes.route.openshift.io'; then
			return 0
		fi
	done
	return 1
}

# has_openshift_password returns success when a kubeadmin password source is available.
has_openshift_password() {
	[[ -n "${OPENSHIFT_PASSWORD:-}" ]] ||
		[[ -n "${KUBEADMIN_PASSWORD_FILE:-}" && -s "${KUBEADMIN_PASSWORD_FILE}" ]] ||
		[[ -n "${SHARED_DIR:-}" && -s "${SHARED_DIR}/kubeadmin-password" ]]
}

if [[ -z "${KONFLUX_PROXY_AUTH:-}" ]]; then
	if [[ "${TEST_ENVIRONMENT:-}" == "upstream" ]]; then
		KONFLUX_PROXY_AUTH=dex
		echo "Proxy auth: dex (TEST_ENVIRONMENT=upstream)"
	elif is_openshift; then
		if ! has_openshift_password; then
			echo "Error: cluster is OpenShift but no kubeadmin password is available." >&2
			echo "       Set OPENSHIFT_PASSWORD, or KUBEADMIN_PASSWORD_FILE, or place" >&2
			echo "       kubeadmin-password in SHARED_DIR." >&2
			echo "       The OpenShift Konflux CR has no Dex static passwords, so the Dex" >&2
			echo "       password grant is not a usable fallback here." >&2
			echo "       To force it anyway: KONFLUX_PROXY_AUTH=dex" >&2
			exit 1
		fi
		KONFLUX_PROXY_AUTH=openshift
		echo "Proxy auth: openshift (detected OpenShift cluster, kubeadmin credentials present)"
	elif has_openshift_password; then
		# The platform probe could not confirm OpenShift, but kubeadmin credentials are
		# present. Keep the pre-existing credential-based inference so a CI run whose
		# cluster is unreachable at this point does not silently fall back to a Dex
		# password grant that has no users behind it.
		KONFLUX_PROXY_AUTH=openshift
		echo "Proxy auth: openshift (kubeadmin credentials present; cluster probe inconclusive)"
	else
		KONFLUX_PROXY_AUTH=dex
		echo "Proxy auth: dex (non-OpenShift cluster)"
	fi
fi
export KONFLUX_PROXY_AUTH

case "${KONFLUX_PROXY_AUTH}" in
openshift)
	echo "Proxy auth: OpenShift OAuth (obtained in test BeforeSuite)"
	export KONFLUX_PROXY_AUTH_METHOD=openshift-oauth
	;;
dex)
	echo "Proxy auth: Dex password grant"
	export KONFLUX_PROXY_AUTH_METHOD=dex-password-grant
	;;
*)
	echo "Error: unsupported KONFLUX_PROXY_AUTH=${KONFLUX_PROXY_AUTH} (expected openshift or dex)" >&2
	exit 1
	;;
esac

cd "${REPO_ROOT}/test/go-tests"
echo "Running proxy integration tests..."
GINKGO_ARGS=()
if [[ "${KONFLUX_PROXY_AUTH}" == "openshift" ]]; then
	GINKGO_ARGS+=(-ginkgo.label-filter='!proxy-dex')
fi
go test -mod=mod . -v -timeout 10m "${GINKGO_ARGS[@]}"
