#!/usr/bin/env bash
set -euo pipefail

# Stop the locally-running Konflux operator (started via "make run" in the
# operator/ directory). Finds the process by its command line and kills only
# that specific process.
#
# Usage:
#   bash skills/dev-verify-loop/scripts/stop-operator.sh

# "make run" executes: go run -ldflags "..." ./cmd/main.go
# go run compiles the code into a temp binary and runs it as a child process.
# We find the go run parent and also kill its children to ensure the compiled
# binary is stopped too.
ALL_PIDS=$(pgrep -f 'go run.*\./cmd/main\.go') || true

if [ -z "${ALL_PIDS}" ]; then
    echo "No running operator process found (looked for 'go run ... ./cmd/main.go')." >&2
    exit 1
fi

PID_COUNT=$(echo "${ALL_PIDS}" | wc -l | tr -d ' ')
if [ "${PID_COUNT}" -gt 1 ]; then
    echo "WARNING: Found ${PID_COUNT} matching operator processes. Stopping all of them." >&2
fi

for FOUND_PID in ${ALL_PIDS}; do
    echo "Found operator process: pid=${FOUND_PID}"

    # Also kill child processes (the compiled binary spawned by go run)
    mapfile -t CHILD_PIDS < <(pgrep -P "${FOUND_PID}" 2>/dev/null) || true

    echo "Stopping operator (pid ${FOUND_PID})..."
    if kill "${FOUND_PID}" "${CHILD_PIDS[@]}" 2>/dev/null; then
        echo "Sent TERM to process ${FOUND_PID} and its children."
    else
        echo "Process ${FOUND_PID} is already stopped."
        continue
    fi

    for _ in $(seq 1 10); do
        if ! ps -p "${FOUND_PID}" > /dev/null 2>&1; then
            echo "Operator stopped (pid ${FOUND_PID})."
            break
        fi
        sleep 0.5
    done

    if ps -p "${FOUND_PID}" > /dev/null 2>&1; then
        echo "WARNING: Process ${FOUND_PID} still running after 5s, sending SIGKILL..." >&2
        kill -9 "${FOUND_PID}" "${CHILD_PIDS[@]}" 2>/dev/null || true
        sleep 1
        if ps -p "${FOUND_PID}" > /dev/null 2>&1; then
            echo "ERROR: Failed to stop process ${FOUND_PID}" >&2
            exit 1
        fi
        echo "Operator stopped (SIGKILL, pid ${FOUND_PID})."
    fi
done

echo "All operator processes stopped."
