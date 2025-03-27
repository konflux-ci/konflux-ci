#!/bin/bash

# Set default values or fallbacks
export SEALIGHTS_TOKEN="${SEALIGHTS_TOKEN:-""}"
export SEALIGHTS_DOMAIN="${SEALIGHTS_DOMAIN:-""}"
export APPLICATION_NAME="${APPLICATION_NAME:-""}"
export BRANCH_NAME="${BRANCH_NAME:-"main"}"
export LAB_ID_FILE_PATH="${LAB_ID_FILE_PATH:-""}"

# Collect missing required environment variables
export MISSING_VARS=()

[[ -z "$SEALIGHTS_TOKEN" ]] && MISSING_VARS+=("SEALIGHTS_TOKEN")
[[ -z "$SEALIGHTS_DOMAIN" ]] && MISSING_VARS+=("SEALIGHTS_DOMAIN")
[[ -z "$APPLICATION_NAME" ]] && MISSING_VARS+=("APPLICATION_NAME")

if [[ ${#MISSING_VARS[@]} -gt 0 ]]; then
  echo "[ERROR] The following required environment variables are missing:"
  for VAR in "${MISSING_VARS[@]}"; do
    echo "  - $VAR"
  done
  exit 1
fi

response=$(curl --silent --show-error --write-out "%{http_code}" --location "$SEALIGHTS_DOMAIN/sl-api/v1/agent-apis/lab-ids" \
  -H "Authorization: Bearer $SEALIGHTS_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{
    "appName": "'"${APPLICATION_NAME}"'",
    "branchName": "'"${BRANCH_NAME}"'",
    "type": "integration",
    "isHidden": false
  }')

# Extract HTTP status (last 3 characters) and body
http_status="${response: -3}"
http_body="${response::-3}"

if [[ "$http_status" -lt 200 || "$http_status" -ge 400 ]]; then
  echo "[ERROR] Sealights API request failed with HTTP status $http_status"
  echo "$http_body"
  exit 1
fi

# Parse labId
LAB_ID=$(echo "$RESPONSE" | jq -r '.data.labId')

# Validate labId
if [[ -z "$LAB_ID" || "$LAB_ID" == "null" ]]; then
  echo "[ERROR] Failed to retrieve labId from response. Full response:"
  echo "$RESPONSE"
  exit 1
else
  echo
