#!/bin/bash

set -euo pipefail

NGINX_AUTH_CONF_FILE='/mnt/nginx-generated-config/auth.conf'
NGINX_NEW_AUTH_CONF_FILE='/mnt/nginx-generated-config/auth.conf.new'

NGINX_CONF_FILE='/etc/nginx/nginx.conf'
RETRY_INTERVAL=10

# test configuration
nginx -g "daemon off;" -c "${NGINX_CONF_FILE}" -t

# run the hot-reload loop in background
(
  # wait for nginx to start before first reload attempt
  while [ ! -f /run/nginx.pid ]; do sleep 1; done

  # retries hot reloading the nginx configuration multiple
  # times with increasing timeouts before returning the error
  reloadWithRetry() {
    if [ -f "${NGINX_NEW_AUTH_CONF_FILE}" ] && \
      ! cmp -s "${NGINX_NEW_AUTH_CONF_FILE}" "${NGINX_AUTH_CONF_FILE}"; then
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

  # not supposed to be terminated
  echo "loop broke, crashing"
  exit 1
) &

# run the nginx server in background
(
  nginx -g "daemon off;" -c "${NGINX_CONF_FILE}"
  echo "nginx crashed"
  exit 1
) &

# wait for any of the background tasks to crash
wait -n
