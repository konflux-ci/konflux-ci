#!/bin/bash

set -euo pipefail

for cmd in date stat; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "required command not found: ${cmd}"; exit 1; }
done

AUTH_CONF_FILE=/mnt/nginx-generated-config/auth.conf
AUTH_CONF_NEW_FILE=/mnt/nginx-generated-config/auth.conf.new

{ [ -f "${AUTH_CONF_NEW_FILE}" ] && [ $(( $(date +%s) - $(stat -c %Y "${AUTH_CONF_NEW_FILE}") )) -lt 60 ]; } || \
  { [ -f "${AUTH_CONF_FILE}" ] && [ $(( $(date +%s) - $(stat -c %Y "${AUTH_CONF_FILE}") )) -lt 60 ]; }
