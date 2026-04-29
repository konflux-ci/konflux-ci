#!/bin/bash -e

# Script to set up release resources for a Konflux application.
# Creates a managed namespace with all required resources (EnterpriseContractPolicy,
# ImageRepositories, ReleasePlanAdmission) and a ReleasePlan in the tenant namespace.
# Note: this script needs to be compatible with Bash 3.x to support both macOS and Linux.

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
  -p, --product-name        Product name to add to the ReleaseNotes (default: --application)
  -v, --product-version     Product version to add to the ReleaseNotes (default: 0.1)
  -c, --component           Component name (repeatable for multiple components;
                            if omitted, auto-detects all components from the application)
  -e, --conforma-policy     EnterpriseContractPolicy name to copy from
                            enterprise-contract-service namespace (default: default)
  -r, --release-name        Name for the ReleasePlan and ReleasePlanAdmission
                            resources (default: local-release)
  -R, --catalog-revision    Release service catalog git revision (default: production)
  -I, --image-name-prefix   Prefix for Quay image repository names. Must be unique
                            across concurrent CI runs to avoid credential collisions.
                            (default: auto-generated from managed namespace + random suffix)
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
PRODUCT_VERSION="0.1"
CONFORMA_POLICY="default"
RELEASE_NAME="local-release"
# renovate: datasource=git-refs depName=https://github.com/konflux-ci/release-service-catalog currentValue=development
CATALOG_REVISION="4944732a48345a94293604b725e7972c251a9271"
IMAGE_NAME_PREFIX=""
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
        -p|--product-name)
            PRODUCT_NAME="$2"
            shift 2
            ;;
        -v|--product-version)
            PRODUCT_VERSION="$2"
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
        -R|--catalog-revision)
            CATALOG_REVISION="$2"
            shift 2
            ;;
        -I|--image-name-prefix)
            IMAGE_NAME_PREFIX="$2"
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

# Parse PRODUCT_NAME default value based on APPLICATION value (after args parsing)
PRODUCT_NAME=${PRODUCT_NAME:-$APPLICATION}

# Generate a unique image name prefix to avoid credential collisions between
# concurrent CI runs that share the same Quay organization.
if [[ -z "${IMAGE_NAME_PREFIX}" ]]; then
    RANDOM_SUFFIX=$(od -An -tx1 -N3 /dev/urandom | tr -d ' ')
    IMAGE_NAME_PREFIX="${MANAGED_NS}-${RANDOM_SUFFIX}"
fi

# OpenShift registers config.openshift.io API resources. Without -o name, kubectl still exits 0
# when the group is absent (header-only table); -o name yields no lines on vanilla k8s / Kind.
IS_OPENSHIFT=false
if [[ -n "$(kubectl api-resources --api-group=config.openshift.io -o name 2>/dev/null)" ]]; then
    IS_OPENSHIFT=true
fi
# Auto-detect components if none specified
if [[ ${#COMPONENTS[@]} -eq 0 ]]; then
    echo "🔍 No components specified, auto-detecting from application '${APPLICATION}' in namespace '${TENANT_NS}'..."
    while IFS= read -r line; do
        COMPONENTS+=("$line")
    done < <(kubectl get components -n "${TENANT_NS}" \
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
echo "   Product Name:      ${PRODUCT_NAME}"
echo "   Product Version:   ${PRODUCT_VERSION}"
echo "   EC policy:         ${CONFORMA_POLICY}"
echo "   Release name:      ${RELEASE_NAME}"
echo "   Catalog revision:  ${CATALOG_REVISION}"
echo "   Image name prefix: ${IMAGE_NAME_PREFIX}"
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

# Step 2: On OpenShift, create a ConfigMap to inject the cluster-wide trusted CA bundle
if [[ "${IS_OPENSHIFT}" == "true" ]]; then
    echo "🔒 OpenShift detected — creating trusted-ca ConfigMap in '${MANAGED_NS}'..."
    kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: trusted-ca
  namespace: ${MANAGED_NS}
  labels:
    config.openshift.io/inject-trusted-cabundle: "true"
data: {}
EOF
fi

# Step 3: Copy EnterpriseContractPolicy from enterprise-contract-service namespace
echo "📜 Copying EnterpriseContractPolicy '${CONFORMA_POLICY}' from enterprise-contract-service namespace..."
kubectl get enterprisecontractpolicy "${CONFORMA_POLICY}" -n enterprise-contract-service -o json \
    | jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp, .metadata.generation, .metadata.managedFields, .metadata.ownerReferences, .status) | .metadata.namespace = "'"${MANAGED_NS}"'"' \
    | kubectl apply -f -

# Step 4: Create RoleBinding for authenticated users
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

# Step 5: Create ImageRepository for trusted-artifacts
echo "🖼️  Creating ImageRepository for trusted-artifacts..."
kubectl apply -f - <<EOF
apiVersion: appstudio.redhat.com/v1alpha1
kind: ImageRepository
metadata:
  name: trusted-artifacts
  namespace: ${MANAGED_NS}
spec:
  image:
    name: ${IMAGE_NAME_PREFIX}/trusted-artifacts
    visibility: public
EOF

# Step 6: Create ImageRepository for each component
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
    name: ${IMAGE_NAME_PREFIX}/${COMPONENT}
    visibility: public
EOF
done

# Step 7: Wait for all ImageRepositories to become ready
echo ""
echo "⏳ Waiting for ImageRepositories to become ready (timeout: ${WAIT_TIMEOUT}s)..."

ALL_REPOS=("trusted-artifacts" "${COMPONENTS[@]}")

for repo in "${ALL_REPOS[@]}"; do
    echo "   Waiting for '${repo}'..."
    wait_for_imagerepository "${repo}"
    echo "   ✅ '${repo}' is ready"
done

# Step 8: Fetch dynamic values from ImageRepository status
echo ""
echo "📡 Fetching image URLs and push secrets from ImageRepository status..."

TA_PUSH_SECRET=$(kubectl get imagerepository trusted-artifacts -n "${MANAGED_NS}" \
    -o jsonpath='{.status.credentials.push-secret}')
TA_IMAGE_URL=$(kubectl get imagerepository trusted-artifacts -n "${MANAGED_NS}" \
    -o jsonpath='{.status.image.url}')
echo "   trusted-artifacts:"
echo "     Image URL:   ${TA_IMAGE_URL}"

# Per-component ImageRepository push secret and image URL; index i matches COMPONENTS[i].
REPO_PUSH_SECRETS=()
REPO_IMAGE_URLS=()
for ((i = 0; i < ${#COMPONENTS[@]}; i++)); do
    COMPONENT="${COMPONENTS[i]}"
    REPO_PUSH_SECRETS[i]=$(kubectl get imagerepository "${COMPONENT}" -n "${MANAGED_NS}" \
        -o jsonpath='{.status.credentials.push-secret}')
    REPO_IMAGE_URLS[i]=$(kubectl get imagerepository "${COMPONENT}" -n "${MANAGED_NS}" \
        -o jsonpath='{.status.image.url}')
    echo "   ${COMPONENT}:"
    echo "     Image URL:   ${REPO_IMAGE_URLS[i]}"
done

# Step 9: Create release-pipeline ServiceAccount with push secrets
echo ""
echo "👤 Creating release-pipeline ServiceAccount..."

# Build the secrets list YAML
SECRETS_YAML="  - name: ${TA_PUSH_SECRET}"
for ((i = 0; i < ${#COMPONENTS[@]}; i++)); do
    SECRETS_YAML="${SECRETS_YAML}
  - name: ${REPO_PUSH_SECRETS[i]}"
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

# Step 10: Create RoleBinding for release-pipeline ServiceAccount
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

# Step 11: Copy SSO credentials from tpa-realm-clients secret in tsf namespace
SSO_SECRET_CREATED=false
SSO_ACCOUNT=release
SSO_TOKEN=$(kubectl get secret tpa-realm-clients -n tsf \
  --ignore-not-found -o jsonpath="{.data.$SSO_ACCOUNT}")
if [ -n "$SSO_TOKEN" ]; then
    echo "🔑 Creating SSO credentials secret from tpa-realm-clients..."
    kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: release-sso-secret
  namespace: ${MANAGED_NS}
type: Opaque
stringData:
  sso_account: ${SSO_ACCOUNT}
data:
  sso_token: ${SSO_TOKEN}
EOF
    SSO_SECRET_CREATED=true
fi
if [ "$SSO_SECRET_CREATED" == false ]; then
    echo "⚠️ Secret 'tpa-realm-client' in namespace 'tsf' not found, skipping SSO credentials creation"
fi

# Step 12: Create ReleasePlanAdmission
echo "📋 Creating ReleasePlanAdmission..."

# Build the components mapping YAML
COMPONENTS_YAML=""
for ((i = 0; i < ${#COMPONENTS[@]}; i++)); do
    COMPONENT="${COMPONENTS[i]}"
    COMPONENTS_YAML="${COMPONENTS_YAML}
        - name: ${COMPONENT}
          repository: ${REPO_IMAGE_URLS[i]}"
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
    releaseNotes:
      product_name: '${PRODUCT_NAME}'
      product_version: '${PRODUCT_VERSION}'
  pipeline:
    pipelineRef:
      resolver: git
      ociStorage: ${TA_IMAGE_URL}
      useEmptyDir: true
      params:
        - name: url
          value: "https://github.com/konflux-ci/release-service-catalog.git"
        - name: revision
          value: ${CATALOG_REVISION}
        - name: pathInRepo
          value: "pipelines/managed/push-to-external-registry/push-to-external-registry.yaml"
    serviceAccountName: release-pipeline
    taskRunSpecs:
      - pipelineTaskName: push-snapshot
        stepSpecs:
          - name: push-snapshot
            computeResources:
              requests:
                cpu: 10m
                memory: 256Mi
              limits:
                memory: 1Gi
EOF

# Step 13: Create ReleasePlan in tenant namespace
echo "📋 Creating ReleasePlan in tenant namespace '${TENANT_NS}'..."
kubectl apply -f - <<EOF
apiVersion: appstudio.redhat.com/v1alpha1
kind: ReleasePlan
metadata:
  labels:
    release.appstudio.openshift.io/auto-release: "true"
    release.appstudio.openshift.io/standing-attribution: "true"
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
if [[ "${IS_OPENSHIFT}" == "true" ]]; then
    echo "  - ConfigMap: trusted-ca (OpenShift trusted CA bundle injection)"
fi
echo "  - EnterpriseContractPolicy: ${CONFORMA_POLICY}"
echo "  - RoleBinding: authenticated-konflux-viewer -> ClusterRole/konflux-viewer-user-actions"
echo "  - ImageRepository: trusted-artifacts"
for COMPONENT in "${COMPONENTS[@]}"; do
    echo "  - ImageRepository: ${COMPONENT}"
done
echo "  - ServiceAccount: release-pipeline (with push secrets)"
echo "  - RoleBinding: release-pipeline-resource-role-binding -> ClusterRole/release-pipeline-resource-role"
if [[ "${SSO_SECRET_CREATED}" == "true" ]]; then
    echo "  - Secret: release-sso-secret (SSO credentials from 'tpa-realm-clients')"
else
    echo "  - Secret: release-sso-secret (SKIPPED - Secret 'tpa-realm-client' in 'tsf' not found)"
fi
echo "  - ReleasePlanAdmission: ${RELEASE_NAME}"
echo ""
echo "Resources created in tenant namespace '${TENANT_NS}':"
echo "  - ReleasePlan: ${RELEASE_NAME} -> ${MANAGED_NS}"
