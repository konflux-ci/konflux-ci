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

# Make the API request and capture HTTP response code
HTTP_RESPONSE=$(curl --silent --show-error --write-out "HTTPSTATUS:%{http_code}" --location "$SEALIGHTS_DOMAIN/sl-api/v1/agent-apis/lab-ids" \
  --header "Content-Type: application/json" \
  --data '{
    "appName": "'${APPLICATION_NAME}'",
    "branchName": "'${BRANCH_NAME}'",
    "type": "integration",
    "isHidden": false
  }')

# Extract body message... TODO!: find a better way to get http status and more simple
HTTP_BODY=$(echo "$HTTP_RESPONSE" | sed -e 's/HTTPSTATUS\:.*//g')
HTTP_STATUS=$(echo "$HTTP_RESPONSE" | tr -d '\n' | sed -e 's/.*HTTPSTATUS://')

# Handle errors
if [[ "$HTTP_STATUS" -ne 200 ]]; then
  echo "[ERROR] Sealights API request failed with status code $HTTP_STATUS"
  echo "[ERROR] Response body:"
  echo "$HTTP_BODY"
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
