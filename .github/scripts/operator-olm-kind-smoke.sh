#!/usr/bin/env bash
# Kind + OLM smoke for konflux-operator (DeployableByOLM class).
#
# Installs the operator via `operator-sdk run bundle` into the CSV suggested
# namespace and asserts CSV Succeeded, the manager Deployment Available, and the
# manager pod Ready without a required Secret metrics-server-cert mount.
# Complements static CSV checks by exercising a real OLM install path.
#
# Usage (from repository root):
#   .github/scripts/operator-olm-kind-smoke.sh
#
# Key env vars:
#   CLUSTER_NAME      Kind cluster name (default: konflux-olm-smoke)
#   REG_NAME/REG_PORT Local registry (default: konflux-olm-smoke-registry / 5005)
#   NS                Install namespace (default: konflux-operator — required)
#   BUILD_MANAGER     If true (default), docker-build + push manager to local registry
#   IMG               Manager image (default: localhost:${REG_PORT}/konflux-operator:ci)
#   KEEP_CLUSTER      If true, skip kind/registry teardown on exit
#   SKIP_CLUSTER_CREATE If true, reuse an existing Kind cluster
#   KIND_NODE_IMAGE   Optional kind node image override (default: kind's default)
#
# Requires: docker (or podman-as-docker), kind, kubectl, make, go (for manager
# build / operator-sdk), curl. operator-sdk is installed via the Makefile if needed.
#
# See: https://github.com/konflux-ci/konflux-ci/issues/8791
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OPERATOR_DIR="${ROOT_DIR}/operator"

CLUSTER_NAME="${CLUSTER_NAME:-konflux-olm-smoke}"
# Avoid host port 5001 — e2e Kind often maps it.
REG_NAME="${REG_NAME:-konflux-olm-smoke-registry}"
REG_PORT="${REG_PORT:-5005}"
# Must match CSV olm.suggested-namespace / olm.operatorNamespace.
NS="${NS:-konflux-operator}"
VERSION="${VERSION:-0.0.0-ci}"
TIMEOUT="${TIMEOUT:-10m}"
CSV_WAIT_TIMEOUT_SEC="${CSV_WAIT_TIMEOUT_SEC:-600}"
BUILD_MANAGER="${BUILD_MANAGER:-true}"
KEEP_CLUSTER="${KEEP_CLUSTER:-false}"
SKIP_CLUSTER_CREATE="${SKIP_CLUSTER_CREATE:-false}"
METRICS_SECRET="metrics-server-cert"
OPERATOR_NAME="konflux-operator"
DEPLOYMENT="konflux-operator-controller-manager"

# Optional override; when unset, use kind's default node image for the installed
# kind version (same approach as operator-test-e2e / deploy-local).
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-}"

CONTAINER_TOOL="${CONTAINER_TOOL:-$(command -v docker >/dev/null 2>&1 && echo docker || echo podman)}"
IMG="${IMG:-localhost:${REG_PORT}/konflux-operator:ci}"
BUNDLE_IMG="${BUNDLE_IMG:-localhost:${REG_PORT}/konflux-operator-bundle:ci}"

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
section() { printf '\n=== %s ===\n' "$*"; }
die() { echo "::error::$*" >&2; exit 1; }

is_podman() {
  if [[ "${CONTAINER_TOOL}" == "podman" ]]; then
    return 0
  fi
  "${CONTAINER_TOOL}" version 2>&1 | grep -qi 'Podman Engine'
}

push_image() {
  local image="$1"
  if is_podman; then
    "${CONTAINER_TOOL}" push --tls-verify=false "${image}"
  else
    "${CONTAINER_TOOL}" push "${image}"
  fi
}

summary() {
  if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
    printf '%s\n' "$*" >>"${GITHUB_STEP_SUMMARY}"
  fi
}

dump_failure_diagnostics() {
  local title="${1:-OLM smoke failed}"
  echo "::group::${title} — diagnostics"
  kubectl config current-context || true
  kubectl get csv -n "${NS}" -o wide 2>/dev/null || true
  kubectl get csv -n "${NS}" -o yaml 2>/dev/null | tail -n 80 || true
  kubectl get deploy,po,secret -n "${NS}" -o wide 2>/dev/null || true
  kubectl describe deploy/"${DEPLOYMENT}" -n "${NS}" 2>/dev/null || true
  kubectl get pods -n "${NS}" -l control-plane=controller-manager -o yaml 2>/dev/null | tail -n 120 || true
  kubectl get events -n "${NS}" --sort-by='.lastTimestamp' 2>/dev/null | tail -n 40 || true
  kubectl get events -n "${NS}" --field-selector reason=FailedMount 2>/dev/null || true
  echo "::endgroup::"

  {
    echo "### ${title}"
    echo ""
    echo '```'
    kubectl get csv -n "${NS}" -o wide 2>/dev/null || echo "(no CSV)"
    kubectl get pods -n "${NS}" -l control-plane=controller-manager -o wide 2>/dev/null || true
    kubectl get events -n "${NS}" --field-selector reason=FailedMount 2>/dev/null || true
    echo '```'
  } >>"${GITHUB_STEP_SUMMARY:-/dev/null}" 2>/dev/null || true
}

cleanup_bundle_side_effects() {
  # Bundle generation mutates tracked kustomize paths; restore only in CI so local
  # runs don't discard uncommitted edits in those files.
  if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
    git -C "${ROOT_DIR}" checkout -- \
      operator/config/manager/kustomization.yaml \
      operator/config/manifests/bases \
      >/dev/null 2>&1 || true
  fi
  rm -rf "${OPERATOR_DIR}/bundle" "${OPERATOR_DIR}/bundle.Dockerfile"
}

teardown() {
  local ec=$?
  cleanup_bundle_side_effects
  if [[ "${KEEP_CLUSTER}" == "true" ]]; then
    log "KEEP_CLUSTER=true — leaving Kind cluster and registry in place"
    exit "${ec}"
  fi
  section "Teardown"
  if command -v operator-sdk >/dev/null 2>&1 || [[ -x "${OPERATOR_DIR}/bin/operator-sdk" ]]; then
    local sdk
    sdk="$(resolve_operator_sdk)"
    "${sdk}" cleanup "${OPERATOR_NAME}" -n "${NS}" --timeout 2m 2>/dev/null || true
  fi
  if kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
    kind delete cluster --name "${CLUSTER_NAME}" || true
  fi
  if "${CONTAINER_TOOL}" inspect "${REG_NAME}" >/dev/null 2>&1; then
    "${CONTAINER_TOOL}" rm -f "${REG_NAME}" >/dev/null 2>&1 || true
  fi
  exit "${ec}"
}

resolve_operator_sdk() {
  if [[ -x "${OPERATOR_DIR}/bin/operator-sdk" ]]; then
    echo "${OPERATOR_DIR}/bin/operator-sdk"
  elif command -v operator-sdk >/dev/null 2>&1; then
    command -v operator-sdk
  else
    die "operator-sdk not found (expected after make operator-sdk)"
  fi
}

ensure_tools() {
  section "Tools"
  command -v "${CONTAINER_TOOL}" >/dev/null || die "${CONTAINER_TOOL} is required"
  command -v kind >/dev/null || die "kind is required"
  command -v kubectl >/dev/null || die "kubectl is required"
  command -v make >/dev/null || die "make is required"

  make -C "${OPERATOR_DIR}" operator-sdk
  OPERATOR_SDK="$(resolve_operator_sdk)"
  log "operator-sdk: ${OPERATOR_SDK} ($("${OPERATOR_SDK}" version 2>/dev/null | head -n1 || true))"
  log "kind: $(kind version | head -n1)"
  log "kubectl: $(kubectl version --client -o yaml 2>/dev/null | awk '/gitVersion:/{print $2; exit}')"
  log "container tool: ${CONTAINER_TOOL} (podman=$(is_podman && echo yes || echo no))"
}

create_registry() {
  if [[ "$("${CONTAINER_TOOL}" inspect -f '{{.State.Running}}' "${REG_NAME}" 2>/dev/null || true)" != "true" ]]; then
    "${CONTAINER_TOOL}" rm -f "${REG_NAME}" >/dev/null 2>&1 || true
    # Start on bridge; connect to kind after the cluster exists (official Kind pattern).
    "${CONTAINER_TOOL}" run \
      -d --restart=always \
      -p "127.0.0.1:${REG_PORT}:5000" \
      --network bridge \
      --name "${REG_NAME}" \
      registry:2
  fi
  log "registry container ${REG_NAME} listening on 127.0.0.1:${REG_PORT}"
}

configure_registry_on_nodes() {
  # Connect registry to the kind network so nodes can reach it.
  if [[ "$("${CONTAINER_TOOL}" inspect -f='{{json .NetworkSettings.Networks.kind}}' "${REG_NAME}" 2>/dev/null || echo null)" == "null" ]]; then
    "${CONTAINER_TOOL}" network connect kind "${REG_NAME}"
  fi

  # Prefer kind-network IP over container DNS name — podman/kind nodes often
  # cannot resolve the registry container hostname.
  local reg_ip
  reg_ip="$("${CONTAINER_TOOL}" inspect -f '{{(index .NetworkSettings.Networks "kind").IPAddress}}' "${REG_NAME}")"
  [[ -n "${reg_ip}" && "${reg_ip}" != "<no value>" ]] || die "could not resolve kind-network IP for ${REG_NAME}"

  local registry_dir="/etc/containerd/certs.d/localhost:${REG_PORT}"
  local node
  for node in $(kind get nodes --name "${CLUSTER_NAME}"); do
    "${CONTAINER_TOOL}" exec "${node}" mkdir -p "${registry_dir}"
    cat <<EOF | "${CONTAINER_TOOL}" exec -i "${node}" cp /dev/stdin "${registry_dir}/hosts.toml"
server = "http://${reg_ip}:5000"

[host."http://${reg_ip}:5000"]
  capabilities = ["pull", "resolve"]
EOF
  done
  log "configured containerd hosts.toml → http://${reg_ip}:5000 for localhost:${REG_PORT}"

  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REG_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
}

create_kind_cluster() {
  section "Kind cluster (${CLUSTER_NAME})"
  create_registry

  if kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
    if [[ "${SKIP_CLUSTER_CREATE}" == "true" ]]; then
      log "reusing existing cluster ${CLUSTER_NAME}"
      kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null
      configure_registry_on_nodes
      return
    fi
    log "deleting existing cluster ${CLUSTER_NAME}"
    kind delete cluster --name "${CLUSTER_NAME}"
  fi

  # Minimal single-node — do not reuse operator/kind-config.yaml (e2e resource/ports).
  local kind_image_args=()
  if [[ -n "${KIND_NODE_IMAGE}" ]]; then
    kind_image_args=(--image "${KIND_NODE_IMAGE}")
  fi
  cat <<EOF | kind create cluster --name "${CLUSTER_NAME}" "${kind_image_args[@]}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
nodes:
- role: control-plane
EOF

  kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null
  kubectl wait --for=condition=Ready nodes --all --timeout=120s
  configure_registry_on_nodes
  log "kubelet $(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}')"
}

install_olm() {
  section "Install OLM"
  if kubectl get crd clusterserviceversions.operators.coreos.com >/dev/null 2>&1; then
    log "OLM CRDs already present"
  else
    "${OPERATOR_SDK}" olm install --timeout "${TIMEOUT}"
  fi
  kubectl -n olm wait --for=condition=Available deploy/olm-operator --timeout=180s
  kubectl -n olm wait --for=condition=Available deploy/catalog-operator --timeout=180s
}

build_and_push_images() {
  section "Build and push images"

  if [[ "${BUILD_MANAGER}" == "true" ]]; then
    log "building manager ${IMG}"
    make -C "${OPERATOR_DIR}" docker-build \
      IMG="${IMG}" \
      VERSION="${VERSION}" \
      CONTAINER_TOOL="${CONTAINER_TOOL}" \
      GIT_COMMIT="$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo ci)"
    push_image "${IMG}"
  else
    log "BUILD_MANAGER=false — using existing IMG=${IMG}"
  fi

  log "generating bundle (VERSION=${VERSION})"
  make -C "${OPERATOR_DIR}" bundle IMG="${IMG}" VERSION="${VERSION}"

  log "building bundle ${BUNDLE_IMG}"
  make -C "${OPERATOR_DIR}" bundle-build \
    BUNDLE_IMG="${BUNDLE_IMG}" \
    CONTAINER_TOOL="${CONTAINER_TOOL}"
  push_image "${BUNDLE_IMG}"
}

run_bundle() {
  section "operator-sdk run bundle (AllNamespaces, ns=${NS})"
  kubectl get ns "${NS}" >/dev/null 2>&1 || kubectl create ns "${NS}"

  # CatalogSource already exists on re-runs without cleanup.
  "${OPERATOR_SDK}" cleanup "${OPERATOR_NAME}" -n "${NS}" --timeout 2m 2>/dev/null || true
  # Namespace may be deleted by cleanup; recreate.
  sleep 2
  kubectl get ns "${NS}" >/dev/null 2>&1 || kubectl create ns "${NS}"

  if ! "${OPERATOR_SDK}" run bundle "${BUNDLE_IMG}" \
    -n "${NS}" \
    --timeout "${TIMEOUT}" \
    --install-mode AllNamespaces \
    --security-context-config=restricted \
    --use-http \
    --skip-tls-verify; then
    dump_failure_diagnostics "operator-sdk run bundle failed"
    die "operator-sdk run bundle failed for ${BUNDLE_IMG}"
  fi
}

wait_csv_succeeded() {
  local csv_name="${OPERATOR_NAME}.v${VERSION}"
  local deadline phase=""
  deadline=$(( $(date +%s) + CSV_WAIT_TIMEOUT_SEC ))
  while (( $(date +%s) < deadline )); do
    if ! phase="$(kubectl get csv "${csv_name}" -n "${NS}" -o jsonpath='{.status.phase}' 2>/dev/null)"; then
      phase=""
    fi
    if [[ "${phase}" == "Succeeded" ]]; then
      log "CSV ${csv_name} phase=Succeeded"
      return 0
    fi
    if [[ "${phase}" == "Failed" ]]; then
      dump_failure_diagnostics "CSV ${csv_name} phase=Failed"
      die "CSV ${csv_name} phase=Failed"
    fi
    log "waiting for CSV ${csv_name} Succeeded (current=${phase:-none})"
    sleep 5
  done
  dump_failure_diagnostics "CSV ${csv_name} did not reach Succeeded (last phase=${phase:-none})"
  die "CSV ${csv_name} did not reach Succeeded within ${CSV_WAIT_TIMEOUT_SEC}s (phase=${phase:-none})"
}

# Names of Deployment volumes that require Secret METRICS_SECRET (optional!=true).
required_metrics_secret_volumes() {
  local name secret optional
  while IFS=$'\t' read -r name secret optional; do
    [[ -z "${secret}" ]] && continue
    [[ "${secret}" != "${METRICS_SECRET}" ]] && continue
    # Missing/empty optional means required (same as optional: false).
    if [[ "${optional}" != "true" ]]; then
      printf '%s\n' "${name}"
    fi
  done < <(
    kubectl -n "${NS}" get deploy "${DEPLOYMENT}" \
      -o jsonpath='{range .spec.template.spec.volumes[*]}{.name}{"\t"}{.secret.secretName}{"\t"}{.secret.optional}{"\n"}{end}'
  )
}

ready_manager_pod_name() {
  local pod_name
  # Prefer a Running pod owned by the current Deployment rollout.
  pod_name="$(
    kubectl -n "${NS}" get pods \
      -l control-plane=controller-manager \
      --field-selector=status.phase=Running \
      -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' \
      | head -n1
  )"
  [[ -n "${pod_name}" ]] || return 1
  printf '%s\n' "${pod_name}"
}

assert_no_metrics_secret_wiring() {
  local volumes mounts secret_vols failed_mounts pod_name

  # Fail closed if the Deployment is missing or unreadable.
  if ! kubectl -n "${NS}" get deploy "${DEPLOYMENT}" -o name >/dev/null; then
    dump_failure_diagnostics "Deployment ${DEPLOYMENT} missing"
    die "Deployment ${DEPLOYMENT} not found"
  fi

  volumes="$(kubectl -n "${NS}" get deploy "${DEPLOYMENT}" \
    -o jsonpath='{.spec.template.spec.volumes}')"
  mounts="$(kubectl -n "${NS}" get deploy "${DEPLOYMENT}" \
    -o jsonpath='{.spec.template.spec.containers[0].volumeMounts}')"

  secret_vols="$(required_metrics_secret_volumes)"
  if [[ -n "${secret_vols}" ]]; then
    dump_failure_diagnostics "Deployment still requires ${METRICS_SECRET}"
    die "Deployment mounts required Secret ${METRICS_SECRET}: ${secret_vols}"
  fi

  if kubectl -n "${NS}" get secret "${METRICS_SECRET}" >/dev/null 2>&1; then
    log "NOTE: Secret ${METRICS_SECRET} exists (unexpected for OLM path); continuing if unused"
  else
    log "OK: Secret ${METRICS_SECRET} absent"
  fi

  # Pod must be Ready — primary live signal DeployableByOLM cares about.
  kubectl -n "${NS}" wait --for=condition=Available "deploy/${DEPLOYMENT}" --timeout=180s
  kubectl -n "${NS}" wait --for=condition=Ready pod \
    -l control-plane=controller-manager --timeout=180s

  pod_name="$(ready_manager_pod_name)" || {
    dump_failure_diagnostics "Manager pod not Ready"
    die "No Running manager pod found for ${DEPLOYMENT}"
  }

  # FailedMount for the metrics secret on the *current* manager pod only.
  # Namespace events from earlier failed installs linger and must not fail a
  # subsequent green run on a reused cluster.
  failed_mounts="$(kubectl -n "${NS}" get events \
    --field-selector "reason=FailedMount,involvedObject.name=${pod_name}" \
    -o jsonpath='{range .items[*]}{.message}{"\n"}{end}' 2>/dev/null || true)"
  if grep -q "${METRICS_SECRET}" <<<"${failed_mounts}"; then
    dump_failure_diagnostics "FailedMount on ${METRICS_SECRET} (pod ${pod_name})"
    die "FailedMount events on ${pod_name} reference ${METRICS_SECRET}"
  fi

  log "OK: deploy Available, pod Ready (${pod_name}), no required ${METRICS_SECRET} volume"
  log "volumes=${volumes:-none} mounts=${mounts:-none}"
}

main() {
  trap teardown EXIT
  ensure_tools
  create_kind_cluster
  install_olm
  build_and_push_images
  run_bundle
  section "Assertions"
  wait_csv_succeeded
  assert_no_metrics_secret_wiring

  summary "### Kind OLM smoke: PASS"
  summary ""
  summary "- CSV Succeeded in \`${NS}\`"
  summary "- \`${DEPLOYMENT}\` Available; manager Ready"
  summary "- No required Secret \`${METRICS_SECRET}\` wiring"
  section "PASS"
  log "OLM smoke succeeded (cluster=${CLUSTER_NAME} ns=${NS} img=${IMG})"
}

main "$@"
