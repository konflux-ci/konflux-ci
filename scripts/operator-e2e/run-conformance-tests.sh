#!/usr/bin/env bash
# Run Ginkgo conformance tests. Requires GH_ORG, GH_TOKEN.
# QUAY_TOKEN is cleared for this invocation. Optional: RELEASE_TA_OCI_STORAGE, E2E_APPLICATIONS_NAMESPACE.
#
# Two modes: source (default, "go test" from the repo) or image (set E2E_CONFORMANCE_IMAGE
# to run the pre-built container as a pod with credentials + kubeconfig injected — same
# contract as podman run, a Tekton step, or a Prow job).
#
# Optional E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS: space-separated extra args appended to the
# test invocation (e.g. -ginkgo.focus=Name -ginkgo.skip=Other); passed to the container
# entrypoint in image mode.
#
# Usage: $0 REPO_ROOT [JUNIT_REPORT_PATH]
set -euo pipefail

REPO_ROOT="$(cd "${1:?usage: $0 REPO_ROOT [JUNIT_REPORT_PATH]}" && pwd)"
JUNIT="${2:-${JUNIT_REPORT_PATH:-${GITHUB_WORKSPACE:-$REPO_ROOT}/junit-conformance.xml}}"

export GITHUB_TOKEN="${GH_TOKEN:?GH_TOKEN required}"
export MY_GITHUB_ORG="${GH_ORG:?GH_ORG required}"
export QUAY_TOKEN=''
export E2E_APPLICATIONS_NAMESPACE="${E2E_APPLICATIONS_NAMESPACE:-default-tenant}"

if [[ -n "${E2E_CONFORMANCE_IMAGE:-}" ]]; then
  echo "Running conformance tests from image: ${E2E_CONFORMANCE_IMAGE}"

  POD_NAME="conformance-runner-$$"
  CREDS_SECRET="conformance-creds-$$"
  KC_FILE="${KUBECONFIG:-$HOME/.kube/config}"
  # Namespace pinned explicitly (kubeconfig default may not exist on target cluster);
  # creds passed via Secret, not env literals, to keep tokens out of `kubectl get pod -o yaml`;
  # --overrides is a JSON merge patch, so every container field must be declared explicit.
  RUNNER_NAMESPACE="${E2E_CONFORMANCE_RUNNER_NAMESPACE:-default}"

  kubectl create secret generic "${CREDS_SECRET}" \
    --namespace="${RUNNER_NAMESPACE}" \
    --from-file=kubeconfig="${KC_FILE}" \
    --from-literal=GITHUB_TOKEN="${GITHUB_TOKEN}" \
    --from-literal=MY_GITHUB_ORG="${MY_GITHUB_ORG}"
  trap 'kubectl delete pod "${POD_NAME}" --namespace="${RUNNER_NAMESPACE}" --ignore-not-found --wait=false 2>/dev/null || true; kubectl delete secret "${CREDS_SECRET}" --namespace="${RUNNER_NAMESPACE}" --ignore-not-found 2>/dev/null || true' EXIT

  # shellcheck disable=SC2086
  kubectl run "${POD_NAME}" \
    --namespace="${RUNNER_NAMESPACE}" \
    --image="${E2E_CONFORMANCE_IMAGE}" \
    --restart=Never \
    --overrides="$(cat <<OVERRIDES
{
  "spec": {
    "containers": [{
      "name": "${POD_NAME}",
      "image": "${E2E_CONFORMANCE_IMAGE}",
      "env": [
        {"name": "GITHUB_TOKEN", "valueFrom": {"secretKeyRef": {"name": "${CREDS_SECRET}", "key": "GITHUB_TOKEN"}}},
        {"name": "MY_GITHUB_ORG", "valueFrom": {"secretKeyRef": {"name": "${CREDS_SECRET}", "key": "MY_GITHUB_ORG"}}},
        {"name": "QUAY_TOKEN", "value": ""},
        {"name": "E2E_APPLICATIONS_NAMESPACE", "value": "${E2E_APPLICATIONS_NAMESPACE}"},
        {"name": "CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE", "value": "${CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE:-}"},
        {"name": "KUBECONFIG", "value": "/etc/creds/kubeconfig"}
      ],
      "volumeMounts": [
        {"name": "creds", "mountPath": "/etc/creds", "readOnly": true}
      ]
    }],
    "volumes": [
      {"name": "creds", "secret": {"secretName": "${CREDS_SECRET}"}}
    ]
  }
}
OVERRIDES
)" \
    -- \
    -ginkgo.vv \
    -test.timeout 45m \
    -test.count 1 \
    ${E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS:-}

  for _ in $(seq 1 120); do
    PHASE=$(kubectl get pod "${POD_NAME}" --namespace="${RUNNER_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Pending")
    case "${PHASE}" in
      Running|Succeeded|Failed) break ;;
    esac
    WAITING_REASON=$(kubectl get pod "${POD_NAME}" --namespace="${RUNNER_NAMESPACE}" \
      -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)
    if [[ "${WAITING_REASON}" == "ImagePullBackOff" || "${WAITING_REASON}" == "ErrImagePull" ]]; then
      echo "ERROR: Pod stuck in ${WAITING_REASON} — conformance image not available." >&2
      kubectl describe pod "${POD_NAME}" --namespace="${RUNNER_NAMESPACE}" >&2 || true
      exit 1
    fi
    sleep 1
  done

  kubectl logs -f "${POD_NAME}" --namespace="${RUNNER_NAMESPACE}" || \
    echo "WARNING: log stream ended early (connection drop?) — falling back to pod exit code." >&2

  for _ in $(seq 1 10); do
    EXIT_CODE=$(kubectl get pod "${POD_NAME}" --namespace="${RUNNER_NAMESPACE}" \
      -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}' 2>/dev/null || true)
    if [[ -n "${EXIT_CODE}" ]]; then
      break
    fi
    sleep 1
  done
  exit "${EXIT_CODE:-1}"
else
  cd "${REPO_ROOT}/test/go-tests"
  # shellcheck disable=SC2086
  go test ./tests/conformance -v -timeout 45m \
    -ginkgo.vv \
    -ginkgo.github-output \
    -ginkgo.junit-report="$JUNIT" \
    ${E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS:-}
fi
