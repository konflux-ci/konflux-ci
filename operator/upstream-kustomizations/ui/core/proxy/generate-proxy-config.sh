#!/bin/sh

set -eu

TEMPLATES_DIR=/mnt/caddy-templates
SNIPPETS_DIR=/mnt/caddy-snippets
SERVICE_CA_PATH=/mnt/service-ca/service-ca.crt

for cmd in sed getent printf; do
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
    if getent hosts "${host}" > /dev/null 2>&1; then
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

# Process kite template
if [ "${KITE_ENABLED:-}" = "true" ] && [ -f "${TEMPLATES_DIR}/kite.caddy" ]; then
  hostname="${KITE_HOSTNAME:-konflux-kite.konflux-kite.svc.cluster.local}"
  sed "s/__KITE_HOSTNAME__/${hostname}/g" \
    "${TEMPLATES_DIR}/kite.caddy" > "${SNIPPETS_DIR}/kite.caddy"
  log "kite endpoint enabled (${hostname})"
fi

# Process kubearchive template
if [ "${KUBEARCHIVE_ENABLED:-}" = "true" ] && [ -f "${TEMPLATES_DIR}/kubearchive.caddy" ]; then
  hostname="${KUBEARCHIVE_HOSTNAME:-kubearchive-api-server.product-kubearchive.svc.cluster.local}"
  sed "s/__KUBEARCHIVE_HOSTNAME__/${hostname}/g" \
    "${TEMPLATES_DIR}/kubearchive.caddy" > "${SNIPPETS_DIR}/kubearchive.caddy"
  log "kubearchive endpoint enabled (${hostname})"
fi

# Process watson template.
# The API_KEY value is cached by file_watcher directly from the mounted Secret;
# the init container only generates the Caddy route and TLS snippets.
if [ "${WATSON_ENABLED:-}" = "true" ] && [ -f "${TEMPLATES_DIR}/watson.caddy" ]; then
  watson_host="${WATSON_HOSTNAME:-api.us-east.assistant.watson.cloud.ibm.com}"
  watson_default_host="api.us-east.assistant.watson.cloud.ibm.com"

  sed "s/__WATSON_HOST__/${watson_host}/g" \
    "${TEMPLATES_DIR}/watson.caddy" > "${SNIPPETS_DIR}/watson.caddy"

  # Generate watson TLS transport config.
  # - Default external host: always use system roots with SNI (never skip verification)
  # - Overridden hostname + service CA: use service CA
  # - Overridden hostname, no service CA: skip verification (in-cluster self-signed)
  if [ "${watson_host}" = "${watson_default_host}" ]; then
    printf 'transport http {\n    tls_server_name %s\n}\n' "${watson_host}" \
      > "${SNIPPETS_DIR}/watson-tls.conf"
    log "watson TLS: system roots with SNI (external host)"
  elif [ -f "${SERVICE_CA_PATH}" ]; then
    printf 'transport http {\n    tls_trust_pool file %s\n}\n' "${SERVICE_CA_PATH}" \
      > "${SNIPPETS_DIR}/watson-tls.conf"
    log "watson TLS: service CA (in-cluster override)"
  else
    printf 'transport http {\n    tls_insecure_skip_verify\n}\n' \
      > "${SNIPPETS_DIR}/watson-tls.conf"
    log "watson TLS: insecure (in-cluster, no service CA)"
  fi

  log "watson endpoint enabled (${watson_host})"
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
