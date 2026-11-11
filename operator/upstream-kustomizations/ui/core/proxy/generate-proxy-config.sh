#!/bin/bash

set -eu

# Defaults match the proxy pod mounts. Override them to run the script in a unit test.
TEMPLATES_DIR=${TEMPLATES_DIR:-/mnt/caddy-templates}
SNIPPETS_DIR=${SNIPPETS_DIR:-/mnt/caddy-snippets}
SERVICE_CA_PATH=${SERVICE_CA_PATH:-/mnt/service-ca/service-ca.crt}

# A DNS name with an optional trailing dot and an optional numeric port.
# Empty is valid: Tekton Results uses it to clear an override. Keep this
# aligned with the kubebuilder Pattern on the proxy hostname fields and with
# proxyBackendHostnamePattern in the UI reconciler.
proxy_hostname_pattern='^$|^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?\.?(:[0-9]{1,5})?$'

for cmd in sed getent printf; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "required command not found: ${cmd}"; exit 1; }
done

log() { echo "$(date -Iseconds) generate-proxy-config: $*"; }

# require_proxy_hostname rejects a value that must not be copied into a Caddyfile.
# The failure log names the variable and omits the value.
require_proxy_hostname() {
  local name=$1
  local value=$2
  if [[ ! ${value} =~ ${proxy_hostname_pattern} ]]; then
    log "invalid ${name}"
    exit 1
  fi
}

# render_template replaces every placeholder in src. The placeholder is a
# fixed string. Callers must pass a value that require_proxy_hostname accepted:
# bash still treats '&' and '\' specially in this expansion.
render_template() {
  local placeholder=$1
  local value=$2
  local src=$3
  local dest=$4
  local template
  template=$(<"${src}")
  template=${template//"$placeholder"/$value}
  printf '%s\n' "${template}" > "${dest}"
}

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
    require_proxy_hostname TEKTON_RESULTS_HOSTNAME "${hostname}"
    log "tekton-results resolved to ${hostname}"
    render_template __TEKTON_RESULTS_HOSTNAME__ "${hostname}" \
      "${TEMPLATES_DIR}/tekton-results.caddy" "${SNIPPETS_DIR}/tekton-results.caddy"
  else
    log "tekton-results not available, skipping"
  fi
fi

# Process kite template
if [ "${KITE_ENABLED:-}" = "true" ] && [ -f "${TEMPLATES_DIR}/kite.caddy" ]; then
  hostname="${KITE_HOSTNAME:-konflux-kite.konflux-kite.svc.cluster.local}"
  require_proxy_hostname KITE_HOSTNAME "${hostname}"
  render_template __KITE_HOSTNAME__ "${hostname}" \
    "${TEMPLATES_DIR}/kite.caddy" "${SNIPPETS_DIR}/kite.caddy"
  log "kite endpoint enabled (${hostname})"
fi

# Process kubearchive template
if [ "${KUBEARCHIVE_ENABLED:-}" = "true" ] && [ -f "${TEMPLATES_DIR}/kubearchive.caddy" ]; then
  hostname="${KUBEARCHIVE_HOSTNAME:-kubearchive-api-server.product-kubearchive.svc.cluster.local}"
  require_proxy_hostname KUBEARCHIVE_HOSTNAME "${hostname}"
  render_template __KUBEARCHIVE_HOSTNAME__ "${hostname}" \
    "${TEMPLATES_DIR}/kubearchive.caddy" "${SNIPPETS_DIR}/kubearchive.caddy"
  log "kubearchive endpoint enabled (${hostname})"
fi

# Process watson template.
# The API_KEY value is cached by file_watcher directly from the mounted Secret;
# the init container only generates the Caddy route and TLS snippets.
if [ "${WATSON_ENABLED:-}" = "true" ] && [ -f "${TEMPLATES_DIR}/watson.caddy" ]; then
  watson_host="${WATSON_HOSTNAME:-api.us-east.assistant.watson.cloud.ibm.com}"
  watson_default_host="api.us-east.assistant.watson.cloud.ibm.com"
  require_proxy_hostname WATSON_HOSTNAME "${watson_host}"

  render_template __WATSON_HOST__ "${watson_host}" \
    "${TEMPLATES_DIR}/watson.caddy" "${SNIPPETS_DIR}/watson.caddy"

  # Generate watson TLS transport config.
  # - Default external host: always use system roots with SNI (never skip verification).
  #   One trailing dot is the same host. SNI stays undotted so it matches the
  #   public certificate. The rendered route keeps the configured hostname.
  # - Overridden hostname + service CA: use service CA
  # - Overridden hostname, no service CA: skip verification (in-cluster self-signed)
  if [ "${watson_host%.}" = "${watson_default_host}" ]; then
    printf 'transport http {\n    tls_server_name %s\n}\n' "${watson_default_host}" \
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

STATIC_DIR=${STATIC_DIR:-/opt/app-root/src/static-content}
out="${STATIC_DIR}/runtime-config.js"
printf 'window.KONFLUX_RUNTIME = window.KONFLUX_RUNTIME || {};\n' > "$out"
env | grep '^RUNTIME_' | sort | while IFS= read -r var; do
  name="${var%%=*}"
  name="${name#RUNTIME_}"
  value="${var#*=}"
  esc=$(printf '%s' "$value" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e "s/$(printf '\r')/\\\\r/g")
  printf 'window.KONFLUX_RUNTIME["%s"] = "%s";\n' "$name" "$esc" >> "$out"
done
log "runtime-config.js generated"

log "done"
