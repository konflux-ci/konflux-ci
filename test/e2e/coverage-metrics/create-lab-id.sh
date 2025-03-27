#!/bin/bash

export SEALIGHTS_TOKEN="${SEALIGHTS_TOKEN:-""}"
export SEALIGHTS_DOMAIN="${SEALIGHTS_DOMAIN:-""}"
export APPLICATION_NAME="${APPLICATION_NAME:-""}"
export BRANCH_NAME="${BRANCH_NAME:-"main"}"
export LAB_ID_FILE_PATH="${LAB_ID_FILE_PATH:-""}"

export MISSING_VARS=()

[[ -z "$SEALIGHTS_TOKEN" ]] && MISSING_VARS+=("SEALIGHTS_TOKEN")
[[ -z "$SEALIGHTS_DOMAIN" ]] && MISSING_VARS+=("SEALIGHTS_DOMAIN")
[[ -z "$SEALIGHTS_TEST_STAGE" ]] && MISSING_VARS+=("SEALIGHTS_TEST_STAGE")

if [[ ${#MISSING_VARS[@]} -gt 0 ]]; then
  echo "[ERROR] The following required environment variables are missing:"
  for VAR in "${MISSING_VARS[@]}"; do
    echo "  - $VAR"
  done
  exit 1
fi

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

LAB_ID=$(echo "$RESPONSE" | jq -r '.data.labId')

if [[ -z "$LAB_ID" || "$LAB_ID" == "null" ]]; then
  echo "[ERROR] Failed to retrieve labId from response. Full response:"
  echo "$RESPONSE"
  exit 1
else
  echo "[INFO] Retrieved labId: $LAB_ID"
fi

if [[ -n "$LAB_ID_FILE_PATH" ]]; then
  mkdir -p "$(dirname "$LAB_ID_FILE_PATH")"

  echo "$LAB_ID" > "$LAB_ID_FILE_PATH"
  echo "[INFO] labId written to \"$LAB_ID_FILE_PATH\""
else
  echo "[WARN] LAB_ID_FILE_PATH is not set. labId not written to file."
fi
