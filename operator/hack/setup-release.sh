#!/bin/bash -e

# Script to set up release resources for a Konflux application.
# Creates a managed namespace with all required resources (EnterpriseContractPolicy,
# ImageRepositories, ReleasePlanAdmission) and a ReleasePlan in the tenant namespace.

set -o pipefail
set -eu

WAIT_TIMEOUT=120  # seconds to wait for ImageRepositories to become ready
POLL_INTERVAL=5   # seconds between polls

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Set up release resources for a Konflux application. Creates a managed namespace
with ImageRepositories, EnterpriseContractPolicy, and ReleasePlanAdmission,
and a ReleasePlan in the tenant namespace.

Options:
  -t, --tenant-namespace    Source tenant namespace (default: default-tenant)
  -m, --managed-namespace   Managed namespace for releases (default: default-managed-tenant)
  -a, --application         Application name (default: sample-component)
  -c, --component           Component name (repeatable for multiple components;
                            if omitted, auto-detects all components from the application)
  -e, --conforma-policy     EnterpriseContractPolicy name to copy from
                            enterprise-contract-service namespace (default: default)
  -r, --release-name        Name for the ReleasePlan and ReleasePlanAdmission
                            resources (default: local-release)
  -h, --help                Show this help message

Examples:
  # Use all defaults with auto-detected components
  $(basename "$0")

  # Set up release for a specific application (auto-detects its components)
  $(basename "$0") -a my-app

  # Specify a custom managed namespace
  $(basename "$0") -m my-managed-ns

  # Explicitly specify components
  $(basename "$0") -c component-a -c component-b

  # Full customization
  $(basename "$0") -t my-tenant -m my-managed -a my-app -c comp1 -c comp2
EOF
    exit 1
}

# Wait for an ImageRepository to reach "ready" state
wait_for_imagerepository() {
    local name="$1"
    local elapsed=0
    while [[ $elapsed -lt $WAIT_TIMEOUT ]]; do
        local state
        state=$(kubectl get imagerepository "${name}" -n "${MANAGED_NS}" \
            -o jsonpath='{.status.state}' 2>/dev/null || true)
        if [[ "${state}" == "ready" ]]; then
            return 0
        fi
        sleep "${POLL_INTERVAL}"
        elapsed=$((elapsed + POLL_INTERVAL))
    done
    echo "Error: ImageRepository '${name}' did not become ready within ${WAIT_TIMEOUT}s"
    echo "  Current state: $(kubectl get imagerepository "${name}" -n "${MANAGED_NS}" -o jsonpath='{.status.state}' 2>/dev/null || echo 'unknown')"
    echo "  Message: $(kubectl get imagerepository "${name}" -n "${MANAGED_NS}" -o jsonpath='{.status.message}' 2>/dev/null || echo 'none')"
    return 1
}

# Parse arguments
TENANT_NS="default-tenant"
MANAGED_NS="default-managed-tenant"
APPLICATION="sample-component"
CONFORMA_POLICY="default"
RELEASE_NAME="local-release"
COMPONENTS=()

while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--tenant-namespace)
            TENANT_NS="$2"
            shift 2
            ;;
        -m|--managed-namespace)
            MANAGED_NS="$2"
            shift 2
            ;;
        -a|--application)
            APPLICATION="$2"
            shift 2
            ;;
        -c|--component)
            COMPONENTS+=("$2")
            shift 2
            ;;
        -e|--conforma-policy)
            CONFORMA_POLICY="$2"
            shift 2
            ;;
        -r|--release-name)
            RELEASE_NAME="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Auto-detect components if none specified
if [[ ${#COMPONENTS[@]} -eq 0 ]]; then
    echo "🔍 No components specified, auto-detecting from application '${APPLICATION}' in namespace '${TENANT_NS}'..."
    mapfile -t COMPONENTS < <(kubectl get components -n "${TENANT_NS}" \
        -o jsonpath="{range .items[?(@.spec.application==\"${APPLICATION}\")]}{.metadata.name}{\"\n\"}{end}" \
        2>/dev/null | grep -v '^$')

    if [[ ${#COMPONENTS[@]} -eq 0 ]]; then
        echo "Error: No components found for application '${APPLICATION}' in namespace '${TENANT_NS}'."
        echo "Make sure the application and its components exist, or specify components explicitly with -c."
        exit 1
    fi
    echo "   Found ${#COMPONENTS[@]} component(s): ${COMPONENTS[*]}"
fi

echo ""
echo "🏗️  Setting up release resources"
echo "   Tenant namespace:  ${TENANT_NS}"
echo "   Managed namespace: ${MANAGED_NS}"
echo "   Application:       ${APPLICATION}"
echo "   EC policy:         ${CONFORMA_POLICY}"
echo "   Release name:      ${RELEASE_NAME}"
echo "   Components:        ${COMPONENTS[*]}"
echo ""

# Step 1: Create managed namespace
echo "📦 Creating managed namespace '${MANAGED_NS}'..."
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${MANAGED_NS}
  labels:
    konflux-ci.dev/type: tenant
EOF

# Step 2: Copy EnterpriseContractPolicy from enterprise-contract-service namespace
echo "📜 Copying EnterpriseContractPolicy '${CONFORMA_POLICY}' from enterprise-contract-service namespace..."
kubectl get enterprisecontractpolicy "${CONFORMA_POLICY}" -n enterprise-contract-service -o json \
    | jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp, .metadata.generation, .metadata.managedFields, .metadata.ownerReferences, .status) | .metadata.namespace = "'"${MANAGED_NS}"'"' \
    | kubectl apply -f -

# Step 3: Create RoleBinding for authenticated users
echo "🔗 Creating RoleBinding for authenticated users..."
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: authenticated-konflux-viewer
  namespace: ${MANAGED_NS}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: konflux-viewer-user-actions
subjects:
  - kind: Group
    name: system:authenticated
    apiGroup: rbac.authorization.k8s.io
EOF

# Step 4: Create ImageRepository for trusted-artifacts
echo "🖼️  Creating ImageRepository for trusted-artifacts..."
kubectl apply -f - <<EOF
apiVersion: appstudio.redhat.com/v1alpha1
kind: ImageRepository
metadata:
  name: trusted-artifacts
  namespace: ${MANAGED_NS}
spec:
  image:
    name: ${MANAGED_NS}/trusted-artifacts
    visibility: public
EOF

# Step 5: Create ImageRepository for each component
for COMPONENT in "${COMPONENTS[@]}"; do
    echo "🖼️  Creating ImageRepository for component '${COMPONENT}'..."
    kubectl apply -f - <<EOF
apiVersion: appstudio.redhat.com/v1alpha1
kind: ImageRepository
metadata:
  name: ${COMPONENT}
  namespace: ${MANAGED_NS}
spec:
  image:
    name: ${MANAGED_NS}/${COMPONENT}
    visibility: public
EOF
done

# Step 6: Wait for all ImageRepositories to become ready
echo ""
echo "⏳ Waiting for ImageRepositories to become ready (timeout: ${WAIT_TIMEOUT}s)..."

ALL_REPOS=("trusted-artifacts" "${COMPONENTS[@]}")

for repo in "${ALL_REPOS[@]}"; do
    echo "   Waiting for '${repo}'..."
    wait_for_imagerepository "${repo}"
    echo "   ✅ '${repo}' is ready"
done

# Step 7: Fetch dynamic values from ImageRepository status
echo ""
echo "📡 Fetching image URLs and push secrets from ImageRepository status..."

TA_PUSH_SECRET=$(kubectl get imagerepository trusted-artifacts -n "${MANAGED_NS}" \
    -o jsonpath='{.status.credentials.push-secret}')
TA_IMAGE_URL=$(kubectl get imagerepository trusted-artifacts -n "${MANAGED_NS}" \
    -o jsonpath='{.status.image.url}')
echo "   trusted-artifacts:"
echo "     Image URL:   ${TA_IMAGE_URL}"

declare -A COMP_PUSH_SECRETS
declare -A COMP_IMAGE_URLS

for COMPONENT in "${COMPONENTS[@]}"; do
    COMP_PUSH_SECRETS["${COMPONENT}"]=$(kubectl get imagerepository "${COMPONENT}" -n "${MANAGED_NS}" \
        -o jsonpath='{.status.credentials.push-secret}')
    COMP_IMAGE_URLS["${COMPONENT}"]=$(kubectl get imagerepository "${COMPONENT}" -n "${MANAGED_NS}" \
        -o jsonpath='{.status.image.url}')
    echo "   ${COMPONENT}:"
    echo "     Image URL:   ${COMP_IMAGE_URLS["${COMPONENT}"]}"
done

# Step 8: Create release-pipeline ServiceAccount with push secrets
echo ""
echo "👤 Creating release-pipeline ServiceAccount..."

# Build the secrets list YAML
SECRETS_YAML="  - name: ${TA_PUSH_SECRET}"
for COMPONENT in "${COMPONENTS[@]}"; do
    SECRETS_YAML="${SECRETS_YAML}
  - name: ${COMP_PUSH_SECRETS["${COMPONENT}"]}"
done

kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: release-pipeline
  namespace: ${MANAGED_NS}
secrets:
${SECRETS_YAML}
EOF

# Step 9: Create RoleBinding for release-pipeline ServiceAccount
echo "🔗 Creating RoleBinding for release-pipeline ServiceAccount..."
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: release-pipeline-resource-role-binding
  namespace: ${MANAGED_NS}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: release-pipeline-resource-role
subjects:
  - kind: ServiceAccount
    name: release-pipeline
    namespace: ${MANAGED_NS}
EOF

# Step 10: Create ReleasePlanAdmission
echo "📋 Creating ReleasePlanAdmission..."

# Build the components mapping YAML
COMPONENTS_YAML=""
for COMPONENT in "${COMPONENTS[@]}"; do
    COMPONENTS_YAML="${COMPONENTS_YAML}
        - name: ${COMPONENT}
          repository: ${COMP_IMAGE_URLS["${COMPONENT}"]}"
done

kubectl apply -f - <<EOF
apiVersion: appstudio.redhat.com/v1alpha1
kind: ReleasePlanAdmission
metadata:
  name: ${RELEASE_NAME}
  namespace: ${MANAGED_NS}
  labels:
    release.appstudio.openshift.io/auto-release: 'true'
spec:
  applications:
    - ${APPLICATION}
  origin: ${TENANT_NS}
  policy: ${CONFORMA_POLICY}
  data:
    mapping:
      defaults:
        pushSourceContainer: false
        tags:
          - latest
          - '{{ git_sha }}'
      components:${COMPONENTS_YAML}
  pipeline:
    pipelineRef:
      resolver: git
      ociStorage: ${TA_IMAGE_URL}
      useEmptyDir: true
      params:
        - name: url
          value: "https://github.com/konflux-ci/release-service-catalog.git"
        - name: revision
          value: production
        - name: pathInRepo
          value: "pipelines/managed/push-to-external-registry/push-to-external-registry.yaml"
    serviceAccountName: release-pipeline
EOF

# Step 11: Create ReleasePlan in tenant namespace
echo "📋 Creating ReleasePlan in tenant namespace '${TENANT_NS}'..."
kubectl apply -f - <<EOF
apiVersion: appstudio.redhat.com/v1alpha1
kind: ReleasePlan
metadata:
  labels:
    release.appstudio.openshift.io/auto-release: "true"
    release.appstudio.openshift.io/standing-attribution: "false"
  name: ${RELEASE_NAME}
  namespace: ${TENANT_NS}
spec:
  application: ${APPLICATION}
  target: ${MANAGED_NS}
EOF

echo ""
echo "✅ Release setup completed successfully!"
echo ""
echo "Resources created in managed namespace '${MANAGED_NS}':"
echo "  - Namespace: ${MANAGED_NS} (with label konflux-ci.dev/type: tenant)"
echo "  - EnterpriseContractPolicy: ${CONFORMA_POLICY}"
echo "  - RoleBinding: authenticated-konflux-viewer -> ClusterRole/konflux-viewer-user-actions"
echo "  - ImageRepository: trusted-artifacts"
for COMPONENT in "${COMPONENTS[@]}"; do
    echo "  - ImageRepository: ${COMPONENT}"
done
echo "  - ServiceAccount: release-pipeline (with push secrets)"
echo "  - RoleBinding: release-pipeline-resource-role-binding -> ClusterRole/release-pipeline-resource-role"
echo "  - ReleasePlanAdmission: ${RELEASE_NAME}"
echo ""
echo "Resources created in tenant namespace '${TENANT_NS}':"
echo "  - ReleasePlan: ${RELEASE_NAME} -> ${MANAGED_NS}"
