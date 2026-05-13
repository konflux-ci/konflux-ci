#!/bin/sh

set -eu

TEMPLATES_DIR=/mnt/caddy-templates
SNIPPETS_DIR=/mnt/caddy-snippets
TOKEN_FILEPATH=/var/run/secrets/konflux-ci.dev/serviceaccount/token
BACKEND_TOKEN_FILEPATH=/var/run/secrets/konflux-ci.dev/backend/token
SERVICE_CA_PATH=/mnt/service-ca/service-ca.crt

for cmd in sed nslookup cat; do
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

  k8s_host=tekton-results-api-service.tekton-pipelines.svc.cluster.local
  ocp_host=tekton-results-api-service.openshift-pipelines.svc.cluster.local

  if nslookup "${k8s_host}" > /dev/null 2>&1; then
    echo "${k8s_host}"
    return 0
  fi
  if nslookup "${ocp_host}" > /dev/null 2>&1; then
    echo "${ocp_host}"
    return 0
  fi

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

# Seed the kube-auth snippet so Caddy can start with a valid Authorization header.
# The config-refresh sidecar will keep this file updated and reload Caddy as needed.
if [ -f "${TOKEN_FILEPATH}" ]; then
  token=$(cat "${TOKEN_FILEPATH}")
  printf 'header_up Authorization "Bearer %s"\n' "${token}" > "${SNIPPETS_DIR}/kube-auth.conf"
  log "seeded kube-auth.conf"
else
  # Create an empty file so Caddy's import doesn't fail on missing file
  touch "${SNIPPETS_DIR}/kube-auth.conf"
  log "warning: token file not found, created empty kube-auth.conf"
fi

# Seed the backend-auth snippet for backend services (Tekton Results, KubeArchive, etc.).
# This token has audience "konflux-backend" and cannot be used against the Kube API directly.
# Backend services validate it via TokenReview.
if [ -f "${BACKEND_TOKEN_FILEPATH}" ]; then
  token=$(cat "${BACKEND_TOKEN_FILEPATH}")
  printf 'header_up Authorization "Bearer %s"\n' "${token}" > "${SNIPPETS_DIR}/backend-auth.conf"
  log "seeded backend-auth.conf"
else
  touch "${SNIPPETS_DIR}/backend-auth.conf"
  log "warning: backend token file not found, created empty backend-auth.conf"
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
