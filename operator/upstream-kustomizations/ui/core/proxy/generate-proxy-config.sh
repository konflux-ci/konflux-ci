#!/bin/sh

set -eu

TEMPLATES_DIR=/mnt/caddy-templates
SNIPPETS_DIR=/mnt/caddy-snippets
SERVICE_CA_PATH=/mnt/service-ca/service-ca.crt

for cmd in sed nslookup; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "required command not found: ${cmd}"; exit 1; }
done

log() { echo "$(date -Iseconds) generate-proxy-config: $*"; }

# resolve_tekton_results outputs the tekton-results hostname.
# Env var TEKTON_RESULTS_HOSTNAME takes precedence over DNS probing.
resolve_tekton_results() {
  if [ -n "${TEKTON_RESULTS_HOSTNAME:-}" ]; then
    echo "${TEKTON_RESULTS_HOSTNAME}"
    return 0
  fi

  for host in \
    tekton-results-api-service.tekton-pipelines.svc.cluster.local \
    tekton-results-api-service.openshift-pipelines.svc.cluster.local \
    tekton-results-api-service.tekton-results.svc.cluster.local; do
    if nslookup "${host}" > /dev/null 2>&1; then
      echo "${host}"
      return 0
    fi
  done

  return 1
}

# Process tekton-results template
if [ -f "${TEMPLATES_DIR}/tekton-results.caddy" ]; then
  if hostname=$(resolve_tekton_results); then
    log "tekton-results resolved to ${hostname}"
    sed "s/__TEKTON_RESULTS_HOSTNAME__/${hostname}/" \
      "${TEMPLATES_DIR}/tekton-results.caddy" > "${SNIPPETS_DIR}/tekton-results.caddy"
  else
    log "tekton-results not available, skipping"
  fi
fi

# Generate TLS transport config for backend services (Tekton Results, KubeArchive, etc.).
# On OpenShift, the service-ca-operator injects the CA bundle into a ConfigMap
# mounted at SERVICE_CA_PATH. When present, backends are verified against this CA.
# Otherwise, fall back to skipping verification (the default for non-OpenShift
# clusters; see docs for how to configure cert-manager to issue trusted certs).
if [ -f "${SERVICE_CA_PATH}" ]; then
  printf 'transport http {\n    tls_trust_pool file %s\n}\n' "${SERVICE_CA_PATH}" \
    > "${SNIPPETS_DIR}/backend-tls.conf"
  log "using service CA for backend TLS verification"
else
  printf 'transport http {\n    tls_insecure_skip_verify\n}\n' \
    > "${SNIPPETS_DIR}/backend-tls.conf"
  log "no service CA found, backend TLS verification disabled"
fi

log "done"
