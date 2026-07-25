#!/usr/bin/env bash
# Probe whether nested mount namespaces work in pods (buildah/unshare).
#
# Prints AppArmor label, capabilities, mount propagation, and `unshare -m`
# for three security postures:
#   1. default (cri-containerd AppArmor / seccomp)
#   2. privileged
#   3. AppArmor + seccomp unconfined (non-privileged)
#
# Used on GHA k3s where Kind succeeds but bare-metal k3s TaskRuns fail with:
#   unshare: cannot change root filesystem propagation: Permission denied
#
# Always exits 0 (informational). Env:
#   DIAG_UNSHARE_NAMESPACE - namespace (default: unshare-diag)
#   DIAG_UNSHARE_IMAGE     - image with bash + unshare (default: ubuntu:24.04)
#
set -euo pipefail

NS="${DIAG_UNSHARE_NAMESPACE:-unshare-diag}"
IMAGE="${DIAG_UNSHARE_IMAGE:-ubuntu:24.04}"

DIAG_BODY=$(
	cat <<'EOS'
set +e
echo "=== identity ==="
id
echo "=== AppArmor current ==="
cat /proc/self/attr/current 2>/dev/null || echo "(no /proc/self/attr/current)"
echo "=== CapEff ==="
grep '^CapEff:' /proc/self/status || true
echo "=== findmnt / ==="
findmnt -o TARGET,PROPAGATION / 2>/dev/null || findmnt / || true
echo "=== unshare -m true ==="
unshare -m true
echo "unshare_exit=$?"
EOS
)

# Indent for YAML literal block under args.
indent_diag() {
	local line
	while IFS= read -r line || [[ -n "${line}" ]]; do
		printf '      %s\n' "${line}"
	done <<<"${DIAG_BODY}"
}

wait_pod_done() {
	local name=$1
	local phase=""
	local _
	for _ in $(seq 1 90); do
		phase="$(kubectl get pod -n "${NS}" "${name}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
		case "${phase}" in
		Succeeded | Failed)
			echo "${phase}"
			return 0
			;;
		esac
		sleep 2
	done
	echo "${phase:-timeout}"
}

run_case() {
	local name=$1
	local manifest=$2
	local ok_var=$3

	kubectl delete pod -n "${NS}" "${name}" --ignore-not-found --wait=true >/dev/null 2>&1 || true
	kubectl apply -n "${NS}" -f - <<<"${manifest}"

	local phase
	phase="$(wait_pod_done "${name}")"
	echo "----- pod/${name} phase=${phase} -----"
	kubectl logs -n "${NS}" "${name}" || true
	local unshare_exit
	unshare_exit="$(kubectl logs -n "${NS}" "${name}" 2>/dev/null | sed -n 's/^unshare_exit=//p' | tail -1 || true)"
	echo "RESULT ${name}: unshare_exit=${unshare_exit:-missing}"
	kubectl delete pod -n "${NS}" "${name}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
	if [[ "${unshare_exit}" == "0" ]]; then
		printf -v "${ok_var}" '%s' 1
	else
		printf -v "${ok_var}" '%s' 0
	fi
}

echo "========================================="
echo "Nested unshare / AppArmor diagnostic"
echo "========================================="
echo "Host sysctls (if present):"
sysctl kernel.apparmor_restrict_unprivileged_userns 2>/dev/null || true
sysctl kernel.apparmor_restrict_unprivileged_unconfined 2>/dev/null || true
if command -v aa-status >/dev/null 2>&1; then
	echo "aa-status (summary):"
	sudo aa-status 2>/dev/null | head -20 || aa-status 2>/dev/null | head -20 || true
fi

kubectl delete namespace "${NS}" --ignore-not-found --wait=true >/dev/null 2>&1 || true
kubectl create namespace "${NS}"

echo "Pre-pulling ${IMAGE}..."
kubectl -n "${NS}" delete pod puller --ignore-not-found --wait=true >/dev/null 2>&1 || true
kubectl -n "${NS}" run puller --image="${IMAGE}" --restart=Never --command -- sleep 3600 >/dev/null
kubectl -n "${NS}" wait --for=condition=Ready pod/puller --timeout=180s
kubectl -n "${NS}" delete pod puller --ignore-not-found --wait=true >/dev/null 2>&1 || true

default_ok=0
priv_ok=0
unconf_ok=0

echo ""
echo "### 1/3 default security context"
run_case unshare-default "$(
	cat <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: unshare-default
spec:
  restartPolicy: Never
  containers:
  - name: diag
    image: ${IMAGE}
    imagePullPolicy: IfNotPresent
    command: ["/bin/bash", "-c"]
    args:
    - |
$(indent_diag)
EOF
)" default_ok

echo ""
echo "### 2/3 privileged: true"
run_case unshare-privileged "$(
	cat <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: unshare-privileged
spec:
  restartPolicy: Never
  containers:
  - name: diag
    image: ${IMAGE}
    imagePullPolicy: IfNotPresent
    securityContext:
      privileged: true
    command: ["/bin/bash", "-c"]
    args:
    - |
$(indent_diag)
EOF
)" priv_ok

echo ""
echo "### 3/3 AppArmor + seccomp Unconfined (non-privileged)"
run_case unshare-unconfined "$(
	cat <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: unshare-unconfined
  annotations:
    container.apparmor.security.beta.kubernetes.io/diag: unconfined
spec:
  restartPolicy: Never
  containers:
  - name: diag
    image: ${IMAGE}
    imagePullPolicy: IfNotPresent
    securityContext:
      allowPrivilegeEscalation: true
      seccompProfile:
        type: Unconfined
      capabilities:
        add: ["SYS_ADMIN", "SYS_CHROOT", "SETUID", "SETGID"]
    command: ["/bin/bash", "-c"]
    args:
    - |
$(indent_diag)
EOF
)" unconf_ok

kubectl delete namespace "${NS}" --ignore-not-found --wait=false >/dev/null 2>&1 || true

echo ""
echo "========================================="
echo "Summary (1=unshare -m ok):"
echo "  default=${default_ok} privileged=${priv_ok} apparmor+seccomp_unconfined=${unconf_ok}"
echo "========================================="
exit 0
