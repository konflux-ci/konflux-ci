#!/usr/bin/env bash
# Copy CLI binaries from task-runner into /mnt/e2e-shared/bin for go-toolset steps
# (go-toolset has go/make but not kubectl, yq, or jq).
set -euo pipefail

DEST="${TEKTON_SHARED_BIN:-/mnt/e2e-shared/bin}"
mkdir -p "${DEST}"
cp -a /usr/local/bin/kubectl /usr/local/bin/yq /usr/bin/jq "${DEST}/"
chmod a+rx "${DEST}/kubectl" "${DEST}/yq" "${DEST}/jq"
