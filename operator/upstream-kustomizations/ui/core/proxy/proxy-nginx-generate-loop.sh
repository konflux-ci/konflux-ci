#!/bin/bash

set -euo pipefail

RETRY_INTERVAL=10
TOKEN_FILEPATH=/var/run/secrets/konflux-ci.dev/serviceaccount/token
AUTH_CONF_FILE=/mnt/nginx-generated-config/auth.conf.new
AUTH_CONF_TMP_FILE=/mnt/nginx-generated-config/auth.conf.new.tmp
AUTH_CONF_TEMPLATE_FILE=/mnt/nginx-templates/auth.conf

for cmd in cat sed chmod mv sleep date; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "required command not found: ${cmd}"; exit 1; }
done

log() { echo "$(date -Iseconds) generate-loop: $*"; }

log "starting"

produceToken() (
  # Copy the auth.conf template and replace the bearer token
  token=$(cat "${TOKEN_FILEPATH}")

  # Produce a tmp file
  sed "s/__BEARER_TOKEN__/${token}/" "${AUTH_CONF_TEMPLATE_FILE}" > "${AUTH_CONF_TMP_FILE}"
  chmod 640 "${AUTH_CONF_TMP_FILE}"

  # Rename (atomic) the file to avoid sync issues
  mv "${AUTH_CONF_TMP_FILE}" "${AUTH_CONF_FILE}"
)

produceTokenWithRetry() (
  produceToken || \
    { sleep 3; produceToken; } || \
    { sleep 5; produceToken; }
)

while produceTokenWithRetry; do sleep "${RETRY_INTERVAL}"; done

echo "loop broke, crashing"
exit 1
