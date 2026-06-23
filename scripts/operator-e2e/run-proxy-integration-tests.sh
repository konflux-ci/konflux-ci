#!/usr/bin/env bash
# Obtain proxy auth tokens (Dex or OpenShift OAuth) and run proxy integration tests.
#
# Usage: run-proxy-integration-tests.sh [REPO_ROOT]
#
# Auth selection:
#   KONFLUX_PROXY_AUTH=openshift|dex (default: infer from CI env)
#   openshift → OAuth authorization-code flow in test BeforeSuite (kubeadmin password required)
#   dex       → proxy tests use Dex password grant at runtime
set -euo pipefail

REPO_ROOT="$(cd "${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}" && pwd)"

if [[ -z "${KONFLUX_PROXY_AUTH:-}" ]]; then
	if [[ "${TEST_ENVIRONMENT:-}" != "upstream" ]] && {
		[[ -n "${OPENSHIFT_PASSWORD:-}" ]] ||
			[[ -n "${KUBEADMIN_PASSWORD_FILE:-}" && -s "${KUBEADMIN_PASSWORD_FILE}" ]] ||
			[[ -n "${SHARED_DIR:-}" && -s "${SHARED_DIR}/kubeadmin-password" ]]
	}; then
		KONFLUX_PROXY_AUTH=openshift
		echo "Proxy auth: inferred openshift (OpenShift CI kubeadmin credentials, TEST_ENVIRONMENT!=upstream)"
	else
		KONFLUX_PROXY_AUTH=dex
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
