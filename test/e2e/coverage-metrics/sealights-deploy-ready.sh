#!/bin/bash

export SEALIGHTS_TOKEN="${SEALIGHTS_TOKEN:-""}"
export SEALIGHTS_LAB_ID="${SEALIGHTS_LAB_ID:-""}"
export SEALIGHTS_DOMAIN="${SEALIGHTS_DOMAIN:-""}"

# Creates a snapshot after finishing the deployment to get Konflux CI complete coverage metric
export SEALIGHTS_BUILD_NAME="$(date +"%Y.%m.%d.%H.%M.%S")"

HTTP_RESPONSE=$(curl --write-out "%{http_code}" --silent --output /dev/null \
  --location "${SEALIGHTS_DOMAIN}/sl-api/v1/agent-apis/lab-ids/${SEALIGHTS_LAB_ID}/integration-build" \
  --header "Authorization: Bearer ${SL_AGENT_TOKEN}" \
  --header 'Content-Type: application/json' \
  --data "{ \"buildName\": \"${SEALIGHTS_BUILD_NAME}\" }")

if [[ "$HTTP_RESPONSE" -ge 200 && "$HTTP_RESPONSE" -lt 300 ]]; then
  echo "[INFO] Curl request was successful. Exiting with status 0."
  exit 0
else
  echo "[ERROR] Curl request failed with status code: $HTTP_RESPONSE"
  exit 1
fi
