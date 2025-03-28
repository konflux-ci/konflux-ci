#!/bin/bash
set -euo pipefail

export SEALIGHTS_TOKEN="${SEALIGHTS_TOKEN:-""}"
export SEALIGHTS_LAB_ID="${SEALIGHTS_LAB_ID:-""}"
export SEALIGHTS_DOMAIN="${SEALIGHTS_DOMAIN:-""}"
export SEALIGHTS_TEST_STAGE="${SEALIGHTS_TEST_STAGE:-""}"
export GINKGO_JSON_REPORT_PATH="${GINKGO_JSON_REPORT_PATH:-""}"
export TEST_SESSION_ID="${TEST_SESSION_ID:-""}"
export TEST_SESSION_ID_FILE="${TEST_SESSION_ID_FILE:-"/tmp/test_session_id"}"

MISSING_VARS=()

[[ -z "$SEALIGHTS_TOKEN" ]] && MISSING_VARS+=("SEALIGHTS_TOKEN")
[[ -z "$SEALIGHTS_LAB_ID" ]] && MISSING_VARS+=("SEALIGHTS_LAB_ID")
[[ -z "$SEALIGHTS_DOMAIN" ]] && MISSING_VARS+=("SEALIGHTS_DOMAIN")
[[ -z "$SEALIGHTS_TEST_STAGE" ]] && MISSING_VARS+=("SEALIGHTS_TEST_STAGE")
[[ -z "$GINKGO_JSON_REPORT_PATH" ]] && MISSING_VARS+=("GINKGO_JSON_REPORT_PATH")

# Exit if any required variables are missing
if [[ ${#MISSING_VARS[@]} -gt 0 ]]; then
  echo "[ERROR] The following required environment variables are missing:"
  for VAR in "${MISSING_VARS[@]}"; do
    echo "  - $VAR"
  done
  exit 1
fi

# Cleanup function to delete the test session
cleanup() {
  if [[ -n "$TEST_SESSION_ID" ]]; then
    echo "[INFO] Closing the test session..."
    curl -s -X DELETE "$SEALIGHTS_DOMAIN/sl-api/v1/test-sessions/$TEST_SESSION_ID" \
      -H "Authorization: Bearer $SEALIGHTS_TOKEN" \
      -H "Content-Type: application/json"
    echo "[INFO] Test session closed successfully"
  fi
}

trap cleanup EXIT

# Load test session ID from file if present. Sealights recommendation is to open session before executing ginkgo code...
if [[ -z "$TEST_SESSION_ID" && -n "$TEST_SESSION_ID_FILE" && -f "$TEST_SESSION_ID_FILE" ]]; then
  FILE_CONTENT=$(<"$TEST_SESSION_ID_FILE")
  if [[ -n "$FILE_CONTENT" ]]; then
    TEST_SESSION_ID="$FILE_CONTENT"
    echo "[INFO] Loaded test session ID from file: $TEST_SESSION_ID_FILE"
  fi
fi

# TODO: Save session id to file if more complex cases comes
if [[ -z "$TEST_SESSION_ID" ]]; then
  echo "[INFO] Creating Sealights test session..."
  TEST_SESSION_ID=$(curl -s -X POST "$SEALIGHTS_DOMAIN/sl-api/v1/test-sessions" \
    -H "Authorization: Bearer $SEALIGHTS_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"labId\":\"$SEALIGHTS_LAB_ID\",\"testStage\":\"$SEALIGHTS_TEST_STAGE\",\"bsid\":\"\",\"sessionTimeout\":10000}" | jq -r '.data.testSessionId')

  if [[ -z "$TEST_SESSION_ID" || "$TEST_SESSION_ID" == "null" ]]; then
    echo "[ERROR] Failed to retrieve test session ID"
    exit 1
  fi
fi

echo "[INFO] Using test session ID: $TEST_SESSION_ID"

# Function to process test report
process_test_report() {
  jq -c '.[] | .SpecReports[]' "$GINKGO_JSON_REPORT_PATH" | while IFS= read -r line; do
    name=$(echo "$line" | jq -r '.LeafNodeText')
    start_raw=$(echo "$line" | jq -r '.StartTime')
    end_raw=$(echo "$line" | jq -r '.EndTime')
    status=$(echo "$line" | jq -r '.State')

    start=$(date --date="$start_raw" +%s%3N)
    end=$( [[ -z "$end_raw" || "$end_raw" == "0001-01-01T00:00:00Z" ]] && date +%s%3N || date --date="$end_raw" +%s%3N )

    if [[ "$status" == "passed" || "$status" == "failed" ]]; then
      echo "{\"name\": \"$name\", \"start\": $start, \"end\": $end, \"status\": \"$status\"}"
    fi
  done | jq -s '.'
}

PROCESSED_JSON=$(process_test_report)
echo "[INFO] Test report processed successfully"

echo "$PROCESSED_JSON" | jq .

echo "[INFO] Sending test results to Sealights..."
curl -s -X POST "https://$SEALIGHTS_DOMAIN/sl-api/v2/test-sessions/$TEST_SESSION_ID" \
  -H "Authorization: Bearer $SEALIGHTS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$PROCESSED_JSON"
