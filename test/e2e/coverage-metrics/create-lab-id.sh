#!/bin/bash

SEALIGHTS_TOKEN="${SEALIGHTS_TOKEN:-""}"
SEALIGHTS_DOMAIN="${SEALIGHTS_DOMAIN:-""}"
APPLICATION_NAME="${APPLICATION_NAME:-""}"
BRANCH_NAME="${BRANCH_NAME:-"main"}"
LAB_ID_FILE_PATH="${LAB_ID_FILE_PATH:-""}"

MISSING_VARS=()
[[ -z "$SEALIGHTS_TOKEN" ]] && MISSING_VARS+=("SEALIGHTS_TOKEN")
[[ -z "$SEALIGHTS_DOMAIN" ]] && MISSING_VARS+=("SEALIGHTS_DOMAIN")
[[ -z "$APPLICATION_NAME" ]] && MISSING_VARS+=("APPLICATION_NAME")

if [[ ${#MISSING_VARS[@]} -gt 0 ]]; then
  for VAR in "${MISSING_VARS[@]}"; do echo "$VAR missing"; done
  exit 1
fi

RESPONSE=$(curl --silent --show-error --write-out "%{http_code}" --location "$SEALIGHTS_DOMAIN/sl-api/v1/agent-apis/lab-ids" \
  -H "Authorization: Bearer $SEALIGHTS_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{
    "appName": "'"$APPLICATION_NAME"'",
    "branchName": "'"$BRANCH_NAME"'",
    "type": "integration",
    "isHidden": false
  }')

# TODO: Is there a better way to get err messages from a curl command?
HTTP_STATUS="${RESPONSE: -3}"
HTTP_BODY="${RESPONSE::-3}"

if [[ "$HTTP_STATUS" -lt 200 || "$HTTP_STATUS" -ge 400 ]]; then
  echo "$HTTP_BODY"
  exit 1
fi

LAB_ID=$(echo "$HTTP_BODY" | jq -r '.data.labId')
[[ -z "$LAB_ID" || "$LAB_ID" == "null" ]] && echo "$HTTP_BODY" && exit 1

# Write labId to file if path provided
if [[ -n "$LAB_ID_FILE_PATH" ]]; then
  mkdir -p "$(dirname "$LAB_ID_FILE_PATH")"
  echo "$LAB_ID" > "$LAB_ID_FILE_PATH"
fi
