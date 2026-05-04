#!/bin/bash

set -euo pipefail

AUTH_CONF_FILE=/mnt/nginx-generated-config/auth.conf
AUTH_CONF_NEW_FILE=/mnt/nginx-generated-config/auth.conf.new

{ [ -f "${AUTH_CONF_NEW_FILE}" ] && [ $(( $(date +%s) - $(stat -c %Y "${AUTH_CONF_NEW_FILE}") )) -lt 60 ]; } || \
  { [ -f "${AUTH_CONF_FILE}" ] && [ $(( $(date +%s) - $(stat -c %Y "${AUTH_CONF_FILE}") )) -lt 60 ]; }
