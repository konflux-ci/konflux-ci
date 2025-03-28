#!/bin/bash

# Ensure necessary environment variables are set
export SEALIGHTS_TOKEN="${SEALIGHTS_TOKEN:-""}"
export SEALIGHTS_LAB_ID="${SEALIGHTS_LAB_ID:-""}"
export SEALIGHTS_DOMAIN="${SEALIGHTS_DOMAIN:-""}"
export SEALIGHTS_TEST_STAGE="${SEALIGHTS_TEST_STAGE:-""}"
export TEST_SESSION_ID_FILE="${TEST_SESSION_ID_FILE:-"/tmp/test_session_id"}"

export MISSING_VARS=()

[[ -z "$SEALIGHTS_TOKEN" ]] && MISSING_VARS+=("SEALIGHTS_TOKEN")
[[ -z "$SEALIGHTS_LAB_ID" ]] && MISSING_VARS+=("SEALIGHTS_LAB_ID")
[[ -z "$SEALIGHTS_DOMAIN" ]] && MISSING_VARS+=("SEALIGHTS_DOMAIN")
[[ -z "$SEALIGHTS_TEST_STAGE" ]] && MISSING_VARS+=("SEALIGHTS_TEST_STAGE")

if [[ ${#MISSING_VARS[@]} -gt 0 ]]; then
  echo "[ERROR] The following required environment variables are missing:"
  for VAR in "${MISSING_VARS[@]}"; do
    echo "  - $VAR"
  done
  exit 1
fi

TEST_SESSION_ID=$(curl -s -X POST "$SEALIGHTS_DOMAIN/sl-api/v1/test-sessions" \
  -H "Authorization: Bearer $SEALIGHTS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg labId "$SEALIGHTS_LAB_ID" --arg testStage "$SEALIGHTS_TEST_STAGE" \
    '{labId: $labId, testStage: $testStage, bsid: "", sessionTimeout: 10000}')" | jq -r '.data.testSessionId')

if [[ -z "$TEST_SESSION_ID" || "$TEST_SESSION_ID" == "null" ]]; then
  echo "[ERROR] Failed to retrieve test session ID"
  exit 1
fi

echo "[INFO] Test session ID: $TEST_SESSION_ID"

if [[ -n "$TEST_SESSION_ID_FILE" ]]; then
  mkdir -p "$(dirname "$TEST_SESSION_ID_FILE")"
fi

echo "$LAB_ID" > "$TEST_SESSION_ID_FILE"
