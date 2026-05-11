#!/bin/bash

set -euo pipefail

NGINX_AUTH_CONF_FILE='/mnt/nginx-generated-config/auth.conf'
NGINX_NEW_AUTH_CONF_FILE='/mnt/nginx-generated-config/auth.conf.new'

NGINX_CONF_FILE='/etc/nginx/nginx.conf'
RETRY_INTERVAL=10

for cmd in nginx cksum mv sleep date; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "required command not found: ${cmd}"; exit 1; }
done

log() { echo "$(date -Iseconds) proxy-nginx-run: $*"; }

log "starting"

# test configuration
nginx -g "daemon off;" -c "${NGINX_CONF_FILE}" -t

# run the hot-reload loop in background
(
  # wait for nginx to start before first reload attempt
  while [ ! -f /run/nginx.pid ]; do sleep 1; done

  log "hot-reload loop started"

  # retries hot reloading the nginx configuration multiple
  # times with increasing timeouts before returning the error
  reloadWithRetry() {
    if [ -f "${NGINX_NEW_AUTH_CONF_FILE}" ] && \
      [ "$(cksum < "${NGINX_NEW_AUTH_CONF_FILE}")" != "$(cksum < "${NGINX_AUTH_CONF_FILE}")" ]; then
      log "config changed, reloading nginx"
      # Move (atomic) the new configuration and reload it in NGINX
      mv "${NGINX_NEW_AUTH_CONF_FILE}" "${NGINX_AUTH_CONF_FILE}" && \
        {
          nginx -s reload || \
            { sleep 3; nginx -s reload; } || \
            { sleep 5; nginx -s reload; }
        }
    fi
  }

  # hot reload infinite loop
  while reloadWithRetry; do sleep "${RETRY_INTERVAL}"; done

  log "loop broke, crashing"
  exit 1
) &
RELOAD_PID=$!

# run the nginx server in background
(
  nginx -g "daemon off;" -c "${NGINX_CONF_FILE}"
  log "nginx crashed"
  exit 1
) &
# forward SIGTERM/SIGINT for graceful shutdown
trap 'log "received signal, shutting down"; nginx -s quit 2>/dev/null; kill "${RELOAD_PID}" 2>/dev/null; wait; exit 0' TERM INT

# wait for any of the background tasks to crash
wait -n
EXIT_CODE=$?
log "child exited with code ${EXIT_CODE}, terminating"
exit "${EXIT_CODE}"
