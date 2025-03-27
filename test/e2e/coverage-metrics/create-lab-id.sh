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

# Make the API request
RESPONSE=$(curl --silent --fail --location "$SEALIGHTS_DOMAIN/sl-api/v1/agent-apis/lab-ids" \
  --header "Content-Type: application/json" \
  --data '{
    "appName": "'${APPLICATION_NAME}'",
    "branchName": "'${BRANCH_NAME}'",
    "type": "integration",
    "isHidden": false
  }')

# Check if curl succeeded
if [[ $? -ne 0 ]]; then
  echo "[ERROR] Failed to reach Sealights API."
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
