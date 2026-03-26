#!/usr/bin/env bash
# Push operator/pkg/manifests (after tekton-deploy-prep.sh / overrides) as an OCI artifact.
#
# Uses the same registry/repo *prefix* as kind-aws provision (param oci-container-repo), but a
# distinct tag: ${PIPELINE_RUN_NAME}.pkg-manifests — provision owns ${repo}:${PIPELINE_RUN_NAME}.
#
# Requires oras and jq (quay.io/konflux-ci/task-runner matches deploy-konflux fetch-kubeconfig image).
#
# Env:
#   E2E_OCI_CONTAINER_REPO   Registry/repo without tag (e.g. quay.io/org/artifacts). If unset/blank, no-op.
#   E2E_PIPELINE_RUN_NAME    Used as tag base (required when repo is set).
#   DOCKER_CONFIG            Directory containing config.json for registry auth (Tekton: mount secret).
#   E2E_PKG_MANIFESTS_OCI_EXPIRATION  Optional quay.expires-after (default: 30d).
#
# Args:
#   $1  Repository root (konflux-ci checkout).
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
MANIFESTS="${REPO_ROOT}/operator/pkg/manifests"

if [[ -z "${E2E_OCI_CONTAINER_REPO// }" ]]; then
	echo "Skipping OCI push of pkg/manifests (E2E_OCI_CONTAINER_REPO unset or blank)."
	exit 0
fi

: "${E2E_PIPELINE_RUN_NAME:?E2E_PIPELINE_RUN_NAME is required when E2E_OCI_CONTAINER_REPO is set}"
: "${DOCKER_CONFIG:?DOCKER_CONFIG must point to a directory containing config.json}"

if [[ ! -f "${DOCKER_CONFIG}/config.json" ]]; then
	echo "error: registry auth file missing: ${DOCKER_CONFIG}/config.json" >&2
	exit 1
fi

if [[ ! -d "$MANIFESTS" ]]; then
	echo "error: manifests directory not found: $MANIFESTS" >&2
	exit 1
fi

command -v oras >/dev/null 2>&1 || {
	echo "error: oras not in PATH" >&2
	exit 1
}

OCI_REPO="${E2E_OCI_CONTAINER_REPO%/}"
OCI_REF="${OCI_REPO}:${E2E_PIPELINE_RUN_NAME}.pkg-manifests"
STAGE="$(mktemp -d)"
ANN="$(mktemp)"
cleanup() {
	rm -rf "${STAGE}"
	rm -f "${ANN}"
}
trap cleanup EXIT

cp -a "${MANIFESTS}" "${STAGE}/manifests"
TITLE="konflux-ci operator pkg/manifests after prep (PipelineRun ${E2E_PIPELINE_RUN_NAME})"
jq -n \
	--arg exp "${E2E_PKG_MANIFESTS_OCI_EXPIRATION:-30d}" \
	--arg title "$TITLE" \
	'{"$manifest": {"quay.expires-after": $exp, "org.opencontainers.image.title": $title}}' >"${ANN}"

cd "${STAGE}"
echo "Pushing operator pkg/manifests to OCI artifact ${OCI_REF} ..."
# Same layer media type as tekton-integration-catalog secure-push-oci (directory as tar).
oras push "${OCI_REF}" --annotation-file "${ANN}" \
	./manifests:application/vnd.acme.rocket.docs.layer.v1+tar
echo "Pushed ${OCI_REF}"
