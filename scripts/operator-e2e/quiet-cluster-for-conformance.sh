#!/usr/bin/env bash
# Scale down UI/Dex/namespace-lister via the Konflux CR and disable Tekton Results
# (including its Postgres) before conformance tests, to free Kind/GHA runner capacity.
#
# Safe to call after proxy + metrics suites. Idempotent.
# Opt out: E2E_QUIET_CLUSTER=false
#
# Usage: quiet-cluster-for-conformance.sh [REPO_ROOT]
set -euo pipefail

REPO_ROOT="$(cd "${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}" && pwd)"
cd "${REPO_ROOT}"

: "${E2E_QUIET_CLUSTER:=true}"
case "${E2E_QUIET_CLUSTER}" in
0 | false | no | FALSE | NO)
	echo "Skipping quiet-cluster-for-conformance (E2E_QUIET_CLUSTER=${E2E_QUIET_CLUSTER})" >&2
	exit 0
	;;
esac

if ! kubectl get konflux konflux >/dev/null 2>&1; then
	echo "Konflux CR 'konflux' not found; skipping quiet-cluster-for-conformance" >&2
	exit 0
fi

echo "Quieting cluster for conformance: scale UI/Dex/namespace-lister to 0, disable Tekton Results..."

# Prefer the Konflux CR when the operator is running (GHA Kind). Also scale
# Deployments directly so this still works when the operator is not (Tekton e2e
# Task runs after bin/manager has exited).
kubectl patch konflux konflux --type=merge -p '{
  "spec": {
    "ui": {
      "spec": {
        "proxy": { "replicas": 0 },
        "dex": { "replicas": 0 }
      }
    },
    "namespaceLister": {
      "spec": {
        "namespaceLister": { "replicas": 0 }
      }
    }
  }
}'

# Direct scale (authoritative when the operator is not reconciling).
for pair in "konflux-ui/proxy" "konflux-ui/dex" "namespace-lister/namespace-lister"; do
	ns="${pair%%/*}"
	name="${pair##*/}"
	if kubectl get deployment -n "${ns}" "${name}" >/dev/null 2>&1; then
		kubectl scale deployment -n "${ns}" "${name}" --replicas=0
	fi
done

# Disable Tekton Results (+ Postgres) via TektonConfig when present (Kind upstream path).
if kubectl get tektonconfig config >/dev/null 2>&1; then
	kubectl patch tektonconfig config --type=merge -p '{"spec":{"result":{"disabled":true}}}'
else
	echo "TektonConfig 'config' not found; skipping Results disable" >&2
fi

# Wait until desired replicas are 0 and no pods remain counted in status.
# status.replicas / availableReplicas may be omitted (empty) once fully scaled down.
wait_workload_zero() {
	local kind="$1" # deployment|statefulset
	local ns="$2"
	local name="$3"
	local timeout="${4:-180}"
	local end=$((SECONDS + timeout))

	if ! kubectl get "${kind}" -n "${ns}" "${name}" >/dev/null 2>&1; then
		echo "  (no ${kind} ${ns}/${name})"
		return 0
	fi

	echo "  Waiting for ${ns}/${name} (${kind}) to reach 0 replicas..."
	while ((SECONDS < end)); do
		local desired status_replicas available
		desired="$(kubectl get "${kind}" -n "${ns}" "${name}" -o jsonpath='{.spec.replicas}' 2>/dev/null || true)"
		status_replicas="$(kubectl get "${kind}" -n "${ns}" "${name}" -o jsonpath='{.status.replicas}' 2>/dev/null || true)"
		available="$(kubectl get "${kind}" -n "${ns}" "${name}" -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)"
		if [[ "${desired}" == "0" ]] &&
			{ [[ -z "${status_replicas}" || "${status_replicas}" == "0" ]]; } &&
			{ [[ -z "${available}" || "${available}" == "0" ]]; }; then
			echo "  ${ns}/${name} is scaled to zero"
			return 0
		fi
		sleep 2
	done

	echo "Timed out waiting for ${ns}/${name} to scale to 0" >&2
	kubectl get "${kind}" -n "${ns}" "${name}" -o wide >&2 || true
	return 1
}

wait_workload_zero deployment konflux-ui proxy 180
wait_workload_zero deployment konflux-ui dex 180
wait_workload_zero deployment namespace-lister namespace-lister 180

# Tear down Results workloads if the Tekton operator has not removed them yet.
if kubectl get namespace tekton-pipelines >/dev/null 2>&1; then
	for deploy in tekton-results-api tekton-results-watcher tekton-results-retention-policy-agent; do
		if kubectl get deployment -n tekton-pipelines "${deploy}" >/dev/null 2>&1; then
			kubectl scale deployment -n tekton-pipelines "${deploy}" --replicas=0 || true
			wait_workload_zero deployment tekton-pipelines "${deploy}" 120
		fi
	done
	if kubectl get statefulset -n tekton-pipelines tekton-results-postgres >/dev/null 2>&1; then
		kubectl scale statefulset -n tekton-pipelines tekton-results-postgres --replicas=0 || true
		wait_workload_zero statefulset tekton-pipelines tekton-results-postgres 120
	fi
fi

echo "Cluster quieted for conformance."
