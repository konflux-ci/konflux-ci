#!/usr/bin/env bash
# Run Ginkgo conformance tests. Requires GH_ORG, GH_TOKEN.
# QUAY_TOKEN is cleared for this invocation. Optional: RELEASE_TA_OCI_STORAGE, E2E_APPLICATIONS_NAMESPACE.
#
# Two modes:
#   Source mode (default): compiles and runs tests via "go test" from the repo.
#   Image mode: set E2E_CONFORMANCE_IMAGE to run the pre-built conformance container
#     against the target cluster. The image is executed as a pod with credentials
#     and kubeconfig injected — the same contract other teams use when running the
#     image via podman run, a Tekton step, or a Prow job.
#
# Optional env E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS: extra arguments appended to
# the test invocation (space-separated), e.g. -ginkgo.focus=Name -ginkgo.skip=Other.
# In image mode these are passed as args to the container entrypoint.
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

  # Store credentials as Kubernetes Secrets rather than --env literals, so tokens
  # are not visible in the Pod spec via kubectl get pod -o yaml.
  kubectl create secret generic "${CREDS_SECRET}" \
    --from-file=kubeconfig="${KC_FILE}" \
    --from-literal=GITHUB_TOKEN="${GITHUB_TOKEN}" \
    --from-literal=MY_GITHUB_ORG="${MY_GITHUB_ORG}"
  trap 'kubectl delete pod "${POD_NAME}" --ignore-not-found --wait=false 2>/dev/null || true; kubectl delete secret "${CREDS_SECRET}" --ignore-not-found 2>/dev/null || true' EXIT

  # Run the conformance image as a proper container with injected credentials.
  # -ginkgo.github-output is intentionally omitted: pod output reaches GH Actions
  # via kubectl logs, not direct stdout, so ::error:: annotations are not parsed.
  # -ginkgo.junit-report is omitted: ubi-minimal lacks tar so kubectl cp cannot
  # extract files from the terminated pod. JUnit is a known gap in image mode;
  # test result is determined by exit code + streamed logs.
  # shellcheck disable=SC2086
  kubectl run "${POD_NAME}" \
    --image="${E2E_CONFORMANCE_IMAGE}" \
    --restart=Never \
    --env="QUAY_TOKEN=" \
    --env="E2E_APPLICATIONS_NAMESPACE=${E2E_APPLICATIONS_NAMESPACE}" \
    --env="CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE=${CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE:-}" \
    --env="KUBECONFIG=/etc/creds/kubeconfig" \
    --overrides="$(cat <<OVERRIDES
{
  "spec": {
    "containers": [{
      "name": "${POD_NAME}",
      "env": [
        {"name": "GITHUB_TOKEN", "valueFrom": {"secretKeyRef": {"name": "${CREDS_SECRET}", "key": "GITHUB_TOKEN"}}},
        {"name": "MY_GITHUB_ORG", "valueFrom": {"secretKeyRef": {"name": "${CREDS_SECRET}", "key": "MY_GITHUB_ORG"}}}
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

  # Wait for pod to start or finish. Detects ImagePullBackOff early instead of
  # waiting the full timeout when the conformance image is unavailable.
  for _ in $(seq 1 120); do
    PHASE=$(kubectl get pod "${POD_NAME}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Pending")
    case "${PHASE}" in
      Running|Succeeded|Failed) break ;;
    esac
    WAITING_REASON=$(kubectl get pod "${POD_NAME}" \
      -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)
    if [[ "${WAITING_REASON}" == "ImagePullBackOff" || "${WAITING_REASON}" == "ErrImagePull" ]]; then
      echo "ERROR: Pod stuck in ${WAITING_REASON} — conformance image not available." >&2
      kubectl describe pod "${POD_NAME}" >&2 || true
      exit 1
    fi
    sleep 1
  done

  # Stream logs (blocks until container exits; works for Running and terminated pods).
  kubectl logs -f "${POD_NAME}"

  # Brief poll for terminated state — kubectl logs -f can return before the API
  # server populates state.terminated.exitCode.
  for _ in $(seq 1 10); do
    EXIT_CODE=$(kubectl get pod "${POD_NAME}" \
      -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}' 2>/dev/null || true)
    if [[ -n "${EXIT_CODE}" ]]; then
      break
    fi
    sleep 1
  done
  exit "${EXIT_CODE:-1}"
else
  cd "${REPO_ROOT}/test/go-tests"
  # Deliberate word-splitting: each space-separated flag must be its own argv token for go test.
  # Quoting ${E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS} would pass one broken argument (e.g. -ginkgo.focus=...).
  # shellcheck disable=SC2086
  go test ./tests/conformance -v -timeout 45m \
    -ginkgo.vv \
    -ginkgo.github-output \
    -ginkgo.junit-report="$JUNIT" \
    ${E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS:-}
fi
