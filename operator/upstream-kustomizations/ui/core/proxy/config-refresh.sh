#!/bin/bash

set -euo pipefail

RETRY_INTERVAL=10
TOKEN_FILEPATH=/var/run/secrets/konflux-ci.dev/serviceaccount/token
BACKEND_TOKEN_FILEPATH=/var/run/secrets/konflux-ci.dev/backend/token
AUTH_SNIPPET=/mnt/caddy-snippets/kube-auth.conf
BACKEND_AUTH_SNIPPET=/mnt/caddy-snippets/backend-auth.conf
CADDYFILE=/etc/caddy/Caddyfile
CADDY_ADMIN=http://localhost:2019

# CA bundle files to watch for content changes. Missing files are
# silently skipped. When any file's content changes, Caddy is reloaded
# so tls_trust_pool picks up the new certificates.
CA_WATCH_PATHS=(
  /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
  /mnt/trusted-ca/ca-bundle.crt
  /mnt/ca.crt
  /mnt/service-ca/service-ca.crt
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

# Read the combined content of all existing CA files. The concatenated
# output is used as a fingerprint — if any file changes, the output differs.
read_ca_content() {
  for f in "${CA_WATCH_PATHS[@]}"; do
    [[ -f "${f}" ]] && cat "${f}"
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
