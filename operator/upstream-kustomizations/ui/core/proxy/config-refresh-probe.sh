#!/bin/sh

set -eu

CADDY_ADMIN=http://localhost:2019
AUTH_SNIPPET=/mnt/caddy-snippets/kube-auth.conf
HEARTBEAT_FILE=/mnt/caddy-snippets/config-refresh-heartbeat
MAX_AGE=30

curl -sf "${CADDY_ADMIN}/config/" > /dev/null 2>&1 || exit 1

[ -s "${AUTH_SNIPPET}" ] || exit 1

# Verify the refresh loop is still running by checking heartbeat age.
[ -f "${HEARTBEAT_FILE}" ] || exit 1
last_beat=$(cat "${HEARTBEAT_FILE}")
now=$(date +%s)
age=$((now - last_beat))
[ "${age}" -le "${MAX_AGE}" ] || exit 1
