#!/usr/bin/env bash
# Copy CLI binaries from task-runner into /mnt/e2e-shared/bin for go-toolset steps
# (go-toolset has go/make but not kubectl, yq, or jq).
# jq is dynamically linked to libjq and libonig; copy those .so files into /mnt/e2e-shared/lib
# and set LD_LIBRARY_PATH in go-toolset scripts (see tekton-run-e2e-tests.sh, deploy-prep, etc.).
#
# task-runner steps run as non-root (taskuser); go-toolset runs as a different non-root UID.
# Widen repo file modes so later go-toolset steps can write (apply-overrides, deploy-prep, etc.).
set -euo pipefail

DEST="${TEKTON_SHARED_BIN:-/mnt/e2e-shared/bin}"
DEST_LIB="${TEKTON_SHARED_LIB:-/mnt/e2e-shared/lib}"
mkdir -p "${DEST}" "${DEST_LIB}"
cp -a /usr/local/bin/kubectl /usr/local/bin/yq /usr/bin/jq "${DEST}/"
# EL10 jq RPM: libjq; libonig comes from oniguruma (symlinks + versioned .so).
cp -a \
  /usr/lib64/libjq.so.1 \
  /usr/lib64/libjq.so.1.0.4 \
  /usr/lib64/libonig.so.5 \
  /usr/lib64/libonig.so.5.4.0 \
  "${DEST_LIB}/"
chmod a+rx "${DEST}/kubectl" "${DEST}/yq" "${DEST}/jq"
chmod -R a+rX "${DEST_LIB}"

REPO_ROOT="${TEKTON_REPO_ROOT:-/mnt/konflux-ci/repo}"
if [[ -d "${REPO_ROOT}" ]]; then
  # chmod -R on REPO_ROOT itself fails: the emptyDir mount point stays root-owned and
  # taskuser cannot change its mode ("Operation not permitted"). Only fix contents.
  find "${REPO_ROOT}" -mindepth 1 -exec chmod a+rwX {} +
fi
