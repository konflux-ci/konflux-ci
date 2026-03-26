#!/usr/bin/env bash
# Run integration tests (test/go-tests packages except conformance).
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT}" && pwd)"
cd "${REPO_ROOT}/test/go-tests"
go test . ./pkg/...
