#!/usr/bin/env bash
#
# Print the default Konflux CR for ./deploy-konflux-on-ocp.sh when KONFLUX_CR is unset.
#
# OpenShift CI (Prow) sets OPENSHIFT_CI=true and needs image-controller plus
# pacWebhookInsecureSSL. Human installs get konflux-openshift.yaml instead.
# Callers that set KONFLUX_CR never invoke this script.
#
# Usage:
#   CR=$(./scripts/default-ocp-konflux-cr.sh REPO_ROOT)

set -euo pipefail

REPO_ROOT="${1:?Usage: default-ocp-konflux-cr.sh REPO_ROOT}"

if [ "${OPENSHIFT_CI:-}" = "true" ]; then
	printf '%s\n' "${REPO_ROOT}/operator/config/samples/konflux-openshift-e2e.yaml"
else
	printf '%s\n' "${REPO_ROOT}/operator/config/samples/konflux-openshift.yaml"
fi
