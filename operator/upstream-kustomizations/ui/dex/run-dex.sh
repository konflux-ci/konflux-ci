#!/bin/bash

set -euo pipefail

ORIGINAL_TOKEN=/tmp/var/run/secrets/dex/serviceaccount/token
TOKEN_PATH=${TOKEN_PATH:-/var/run/secrets/kubernetes.io/serviceaccount/token}
WAIT_TIME=10
HALT_TIMEOUT=100
MAX_JITTER_TIME=10

# auxiliary log function
log() { printf "%s: %s\n" "$(date -Iseconds)" "$*"; }

# forward SIGTERM/SIGINT for graceful shutdown
shutdown() {
  log "received signal, shutting down"
  if [ -n "${dex_pid:-}" ] && [ -d "/proc/${dex_pid}" ]; then
    kill -s TERM "${dex_pid}" 2>/dev/null || true
    wait_for_halt || true
  fi
  exit 143
}

wait_for_halt() (
  counter=0
  while [ -d "/proc/${dex_pid}" ] && [ $(( counter++ )) -le "${HALT_TIMEOUT}" ]; do 
    sleep 1
  done

  if [ -d "/proc/${dex_pid}" ]; then
    return 1
  fi
  return 0
)

start_with_reload() {
  while true; do
    # copy the original token for later use
    log "copying the token to ${ORIGINAL_TOKEN}"
    mkdir -p "$(dirname "${ORIGINAL_TOKEN}")"
    rm "${ORIGINAL_TOKEN}" || true
    cp "${TOKEN_PATH}" "${ORIGINAL_TOKEN}"

    # run dex in a subprocess
    OPENSHIFT_OAUTH_CLIENT_SECRET="$(cat "${ORIGINAL_TOKEN}")" \
    OPENSHIFT_OAUTH_CLIENT_ID="${OPENSHIFT_OAUTH_CLIENT_ID:-system:serviceaccount:${NAMESPACE}:${POD_SERVICE_ACCOUNT}}" \
      /usr/local/bin/dex serve /etc/dex/cfg/config.yaml &
    dex_pid="$!"
    log "run Dex in background with pid ${dex_pid}"

    # wait for the token to be refresh by kubelet
    log "waiting for the token to be refreshed"
    while [ "$(cksum < "${ORIGINAL_TOKEN}")" = "$(cksum < "${TOKEN_PATH}")" ]; do sleep "${WAIT_TIME}"; done

    # reduce likelihood of having all replicas refreshing at the same time
    jitter="$(( $(od -An -N2 -i /dev/urandom) % MAX_JITTER_TIME ))"
    log "sleeping ${jitter} seconds to prevent all replicas to restart at the same time"
    sleep "${jitter}"

    # Send SIGTERM to Dex
    log "token refreshed, halting Dex via SIGTERM"
    if [ -d "/proc/${dex_pid}" ]; then
      kill -s TERM "${dex_pid}" || true
    fi

    # Wait for Dex to halt
    log "waiting for Dex to gracefully stop"
    if ! wait_for_halt; then
      log "Dex didn't stop in ${HALT_TIMEOUT} seconds, breaking"
      exit 1
    fi
  done

  log "the loop broke unexpectedly"
  exit 1
}

main() {
  trap shutdown TERM INT

  if [ "${OPENSHIFT_LOGIN_ENABLED:-false}" == "true" ]; then
    # if it is running on openshift, the OpenShift OAuth2 client
    # is registered and ServiceAccount token reloading is enabled
    start_with_reload
  else
    # otherwise there is no need for reloading the token
    /usr/local/bin/dex serve /etc/dex/cfg/config.yaml &
    dex_pid="$!"
    wait "${dex_pid}"
  fi
}

main
