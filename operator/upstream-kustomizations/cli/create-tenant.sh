#!/bin/bash -e

# Script to create a new Konflux tenant namespace with all required resources

usage() {
    cat <<EOF
Usage: $(basename "$0") -n <namespace> -u <admin-user>

Create a new Konflux tenant namespace with all required resources.

Required arguments:
  -n, --namespace    Name of the tenant namespace to create
  -u, --admin-user   Name of the admin user (e.g., user1@konflux.dev)

Example:
  $(basename "$0") -n my-tenant -u user1@konflux.dev
EOF
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -u|--admin-user)
            ADMIN_USER="$2"
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

# Validate required arguments
if [[ -z "${NAMESPACE}" ]]; then
    echo "Error: Namespace is required"
    usage
fi

if [[ -z "${ADMIN_USER}" ]]; then
    echo "Error: Admin user is required"
    usage
fi

echo "🏗️  Creating Konflux tenant namespace: ${NAMESPACE}"

# Create the namespace with tenant label
echo "📦 Creating namespace..."
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
  labels:
    konflux-ci.dev/type: tenant
EOF

# Create the konflux-integration-runner ServiceAccount
echo "👤 Creating konflux-integration-runner ServiceAccount..."
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: konflux-integration-runner
  namespace: ${NAMESPACE}
EOF

# Create RoleBinding for konflux-integration-runner ServiceAccount
echo "🔗 Creating RoleBinding for konflux-integration-runner..."
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: konflux-integration-runner
  namespace: ${NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: konflux-integration-runner
subjects:
- kind: ServiceAccount
  name: konflux-integration-runner
  namespace: ${NAMESPACE}
EOF

# Create RoleBinding for admin user
echo "👑 Creating RoleBinding for admin user: ${ADMIN_USER}..."
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ${ADMIN_USER%%@*}-konflux-admin
  namespace: ${NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: konflux-admin-user-actions
subjects:
- kind: User
  name: ${ADMIN_USER}
  apiGroup: rbac.authorization.k8s.io
EOF

echo "✅ Tenant namespace '${NAMESPACE}' created successfully!"
echo ""
echo "Resources created:"
echo "  - Namespace: ${NAMESPACE} (with label konflux-ci.dev/type: tenant)"
echo "  - ServiceAccount: konflux-integration-runner"
echo "  - RoleBinding: konflux-integration-runner -> ClusterRole/konflux-integration-runner"
echo "  - RoleBinding: ${ADMIN_USER%%@*}-konflux-admin -> ClusterRole/konflux-admin-user-actions"
