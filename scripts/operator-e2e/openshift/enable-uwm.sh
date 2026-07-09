#!/usr/bin/env bash
# Enable OpenShift user-workload monitoring (UWM) when not already configured.
#
# Idempotent: safe on clusters where external GitOps or preview already applied
# cluster-monitoring-config with user-workload monitoring enabled.
#
# When patching an existing cluster-monitoring-config, the merge patch replaces the entire
# config.yaml data key (does not merge YAML fields). Safe on ephemeral CI clusters; clusters
# with other monitoring settings in config.yaml need read-modify-write instead.
set -euo pipefail

OC="${OC:-oc}"
NS="openshift-monitoring"
CM="cluster-monitoring-config"

if ! command -v "${OC}" &>/dev/null; then
  echo "ERROR: ${OC} not found" >&2
  exit 1
fi

if ! "${OC}" get namespace "${NS}" &>/dev/null; then
  echo "ERROR: namespace ${NS} not found (not an OpenShift cluster?)" >&2
  exit 1
fi

if "${OC}" get configmap "${CM}" -n "${NS}" &>/dev/null; then
  if "${OC}" get configmap "${CM}" -n "${NS}" -o yaml | grep -q 'enableUserWorkload: true'; then
    echo "[INFO] ${CM} already has enableUserWorkload: true"
    exit 0
  fi
  echo "[INFO] Patching ${CM} to set enableUserWorkload: true"
  "${OC}" patch configmap "${CM}" -n "${NS}" --type merge -p \
    '{"data":{"config.yaml":"enableUserWorkload: true\n"}}'
  exit 0
fi

echo "[INFO] Creating ${CM} with enableUserWorkload: true"
"${OC}" apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-monitoring-config
  namespace: openshift-monitoring
data:
  config.yaml: |
    enableUserWorkload: true
EOF
