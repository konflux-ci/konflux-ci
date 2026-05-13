#!/bin/bash

set -euo pipefail

RETRY_INTERVAL=10
TOKEN_FILEPATH=/var/run/secrets/konflux-ci.dev/serviceaccount/token
BACKEND_TOKEN_FILEPATH=/var/run/secrets/konflux-ci.dev/backend/token
AUTH_SNIPPET=/mnt/caddy-snippets/kube-auth.conf
BACKEND_AUTH_SNIPPET=/mnt/caddy-snippets/backend-auth.conf
CADDYFILE=/etc/caddy/Caddyfile
CADDY_ADMIN=http://localhost:2019

# Directories to scan for certificate files (*.crt, *.pem).
# Any volume mounted under these paths is automatically watched;
# adding a new CA volume only requires mounting it to this container.
CA_WATCH_DIRS=(
  /var/run/secrets/kubernetes.io/serviceaccount
  /mnt/trusted-ca
  /mnt/service-ca
  /mnt/serving-cert
)

HEARTBEAT_FILE=/mnt/caddy-snippets/config-refresh-heartbeat

CURRENT_TOKEN=""
CURRENT_BACKEND_TOKEN=""
CURRENT_CA_CONTENT=""

for cmd in curl cat sleep date; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "required command not found: ${cmd}"; exit 1; }
done

log() { echo "$(date -Iseconds) config-refresh: $*"; }

wait_for_caddy() {
  log "waiting for Caddy admin API..."
  until curl -sf "${CADDY_ADMIN}/config/" > /dev/null 2>&1; do
    sleep 1
  done
  log "Caddy admin API ready"
}

reload_caddy() {
  curl -sf -X POST "${CADDY_ADMIN}/load" \
    -H "Content-Type: text/caddyfile" \
    --data-binary @"${CADDYFILE}" || {
      log "error: failed to reload Caddy config"
      return 1
    }
}

# Read the combined content of all cert files found under CA_WATCH_DIRS.
# The concatenated output is used as a fingerprint — if any file changes,
# the output differs and triggers a Caddy reload.
read_ca_content() {
  for dir in "${CA_WATCH_DIRS[@]}"; do
    [[ -d "${dir}" ]] || continue
    for f in "${dir}"/*.crt "${dir}"/*.pem; do
      [[ -f "${f}" ]] && cat "${f}"
    done
  done
  true
}

# Check for token, backend token, and CA changes, reload Caddy when needed.
refresh_cycle() {
  changed=""

  new_token=$(cat "${TOKEN_FILEPATH}")
  if [ "${new_token}" != "${CURRENT_TOKEN}" ]; then
    printf 'header_up Authorization "Bearer %s"\n' "${new_token}" > "${AUTH_SNIPPET}"
    CURRENT_TOKEN="${new_token}"
    changed="kube token"
  fi

  if [[ -f "${BACKEND_TOKEN_FILEPATH}" ]]; then
    new_backend_token=$(cat "${BACKEND_TOKEN_FILEPATH}")
    if [ "${new_backend_token}" != "${CURRENT_BACKEND_TOKEN}" ]; then
      printf 'header_up Authorization "Bearer %s"\n' "${new_backend_token}" > "${BACKEND_AUTH_SNIPPET}"
      CURRENT_BACKEND_TOKEN="${new_backend_token}"
      changed="${changed:+${changed}, }backend token"
    fi
  fi

  new_ca=$(read_ca_content)
  if [ "${new_ca}" != "${CURRENT_CA_CONTENT}" ]; then
    CURRENT_CA_CONTENT="${new_ca}"
    changed="${changed:+${changed}, }CA bundles"
  fi

  if [ -n "${changed}" ]; then
    reload_caddy
    log "reloaded (${changed} changed)"
  fi
}

refresh_cycle_with_retry() {
  refresh_cycle || \
    { sleep 3; refresh_cycle; } || \
    { sleep 5; refresh_cycle; }
}

wait_for_caddy

while refresh_cycle_with_retry; do
  date +%s > "${HEARTBEAT_FILE}"
  sleep "${RETRY_INTERVAL}"
done

log "loop broke, crashing"
exit 1
