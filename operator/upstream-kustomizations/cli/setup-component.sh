#!/bin/bash -e

# Script to set up application/component onboarding resources for a Konflux component.
# Creates Application, Component, Pipelines-as-Code Repository, and optional
# IntegrationTestScenario resources in a tenant namespace.
# Note: this script needs to be compatible with Bash 3.x to support both macOS and Linux.

set -o pipefail
set -eu

WAIT_TIMEOUT=180  # seconds to wait for build pipeline service account
POLL_INTERVAL=5   # seconds between polls

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Set up onboarding resources for a Konflux application component.

Options:
  -t, --tenant-namespace        Tenant namespace (default: default-tenant)
  -a, --application             Application name (default: sample-component)
  -c, --component               Component name (default: sample-component)
  -g, --git-url                 Component git URL (default: https://github.com/konflux-ci/testrepo)
  -r, --git-revision            Component git revision/branch (default: main)
  -x, --git-context             Component git context path (default: ./)
  -d, --dockerfile              Dockerfile path (default: Dockerfile)
  -u, --repository-url          Pipelines-as-Code repository URL (default: git-url without trailing .git)
  -s, --skip-repository         Skip creating Pipelines-as-Code Repository resource
  -p, --build-pipeline          Build pipeline annotation JSON value for
                                build.appstudio.openshift.io/pipeline
  -M, --registry-mode           Registry mode: image-controller|in-cluster (default: image-controller)
  -i, --integration-git-url     Integration test git URL (optional; requires -j and -k)
  -j, --integration-revision    Integration test git revision (optional; requires -i and -k)
  -k, --integration-path        Integration test pathInRepo (optional; requires -i and -j)
  -h, --help                    Show this help message

Examples:
  # Minimal setup with defaults
  $(basename "$0")

  # Configure a component on a custom branch with in-cluster registry mode
  $(basename "$0") -t user-ns2 -a testrepo -c testrepo \\
    -g https://github.com/my-org/testrepo -r my-branch -M in-cluster

  # Include integration test scenario
  $(basename "$0") -a my-app -c my-component \\
    -i https://github.com/my-org/testrepo -j main -k integration-tests/my-test.yaml
EOF
    exit 1
}

strip_git_suffix() {
    local input="$1"
    input="${input%/}"
    echo "${input%.git}"
}

TENANT_NS="default-tenant"
APPLICATION="sample-component"
COMPONENT="sample-component"
GIT_URL="https://github.com/konflux-ci/testrepo"
GIT_REVISION="main"
GIT_CONTEXT="./"
DOCKERFILE_PATH="Dockerfile"
REPOSITORY_URL=""
SKIP_REPOSITORY=false
BUILD_PIPELINE_ANNOTATION=""
REGISTRY_MODE="image-controller"
INTEGRATION_GIT_URL=""
INTEGRATION_REVISION=""
INTEGRATION_PATH=""
REGISTRY_SECRET_NAME="regcred-empty"

while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--tenant-namespace)
            TENANT_NS="$2"
            shift 2
            ;;
        -a|--application)
            APPLICATION="$2"
            shift 2
            ;;
        -c|--component)
            COMPONENT="$2"
            shift 2
            ;;
        -g|--git-url)
            GIT_URL="$2"
            shift 2
            ;;
        -r|--git-revision)
            GIT_REVISION="$2"
            shift 2
            ;;
        -x|--git-context)
            GIT_CONTEXT="$2"
            shift 2
            ;;
        -d|--dockerfile)
            DOCKERFILE_PATH="$2"
            shift 2
            ;;
        -u|--repository-url)
            REPOSITORY_URL="$2"
            shift 2
            ;;
        -s|--skip-repository)
            SKIP_REPOSITORY=true
            shift 1
            ;;
        -p|--build-pipeline)
            BUILD_PIPELINE_ANNOTATION="$2"
            shift 2
            ;;
        -M|--registry-mode)
            REGISTRY_MODE="$2"
            shift 2
            ;;
        -i|--integration-git-url)
            INTEGRATION_GIT_URL="$2"
            shift 2
            ;;
        -j|--integration-revision)
            INTEGRATION_REVISION="$2"
            shift 2
            ;;
        -k|--integration-path)
            INTEGRATION_PATH="$2"
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

wait_for_serviceaccount() {
    local sa_name="$1"
    local elapsed=0
    while [[ $elapsed -lt $WAIT_TIMEOUT ]]; do
        if kubectl get serviceaccount "${sa_name}" -n "${TENANT_NS}" >/dev/null 2>&1; then
            return 0
        fi
        sleep "${POLL_INTERVAL}"
        elapsed=$((elapsed + POLL_INTERVAL))
    done
    echo "Error: ServiceAccount '${sa_name}' was not created in namespace '${TENANT_NS}' within ${WAIT_TIMEOUT}s"
    return 1
}

if [[ "${REGISTRY_MODE}" != "image-controller" && "${REGISTRY_MODE}" != "in-cluster" ]]; then
    echo "Error: --registry-mode must be one of: image-controller, in-cluster"
    exit 1
fi

if [[ -z "${REPOSITORY_URL}" ]]; then
    REPOSITORY_URL="$(strip_git_suffix "${GIT_URL}")"
fi

has_any_integration_value=false
has_all_integration_values=false
if [[ -n "${INTEGRATION_GIT_URL}" || -n "${INTEGRATION_REVISION}" || -n "${INTEGRATION_PATH}" ]]; then
    has_any_integration_value=true
fi
if [[ -n "${INTEGRATION_GIT_URL}" && -n "${INTEGRATION_REVISION}" && -n "${INTEGRATION_PATH}" ]]; then
    has_all_integration_values=true
fi
if [[ "${has_any_integration_value}" == "true" && "${has_all_integration_values}" != "true" ]]; then
    echo "Error: integration options must be provided together (-i, -j, -k)"
    exit 1
fi

echo ""
echo "Setting up component onboarding resources"
echo "  Tenant namespace:  ${TENANT_NS}"
echo "  Application:       ${APPLICATION}"
echo "  Component:         ${COMPONENT}"
echo "  Git URL:           ${GIT_URL}"
echo "  Git revision:      ${GIT_REVISION}"
echo "  Git context:       ${GIT_CONTEXT}"
echo "  Dockerfile path:   ${DOCKERFILE_PATH}"
echo "  Repository URL:    ${REPOSITORY_URL}"
echo "  Skip Repository:   ${SKIP_REPOSITORY}"
echo "  Registry secret:   ${REGISTRY_SECRET_NAME}"
echo "  Registry mode:     ${REGISTRY_MODE}"
if [[ -n "${BUILD_PIPELINE_ANNOTATION}" ]]; then
    echo "  Build pipeline:    custom annotation provided"
else
    echo "  Build pipeline:    default operator behavior"
fi
if [[ "${has_all_integration_values}" == "true" ]]; then
    echo "  Integration test:  ${INTEGRATION_GIT_URL}@${INTEGRATION_REVISION}:${INTEGRATION_PATH}"
else
    echo "  Integration test:  not configured"
fi
echo ""

IMAGE_ANNOTATIONS=""
if [[ "${REGISTRY_MODE}" == "image-controller" ]]; then
    IMAGE_ANNOTATIONS='
    image.redhat.com/generate: "{\"visibility\": \"public\"}"'
fi

PIPELINE_ANNOTATION=""
if [[ -n "${BUILD_PIPELINE_ANNOTATION}" ]]; then
    PIPELINE_ANNOTATION_ESCAPED=$(printf '%s' "${BUILD_PIPELINE_ANNOTATION}" | sed 's/\\/\\\\/g; s/"/\\"/g')
    PIPELINE_ANNOTATION='
    build.appstudio.openshift.io/pipeline: "'"${PIPELINE_ANNOTATION_ESCAPED}"'"'
fi

echo "Applying Application + Component resources..."
kubectl apply -f - <<EOF
apiVersion: appstudio.redhat.com/v1alpha1
kind: Application
metadata:
  name: ${APPLICATION}
  namespace: ${TENANT_NS}
spec:
  displayName: ${APPLICATION}
---
apiVersion: appstudio.redhat.com/v1alpha1
kind: Component
metadata:
  name: ${COMPONENT}
  namespace: ${TENANT_NS}
  annotations:
    build.appstudio.openshift.io/request: configure-pac${PIPELINE_ANNOTATION}${IMAGE_ANNOTATIONS}
spec:
  application: ${APPLICATION}
  componentName: ${COMPONENT}
  source:
    git:
      url: ${GIT_URL}
      revision: ${GIT_REVISION}
      context: ${GIT_CONTEXT}
      dockerfileUrl: ${DOCKERFILE_PATH}
EOF

if [[ "${SKIP_REPOSITORY}" != "true" ]]; then
    echo "Applying Pipelines-as-Code Repository resource..."
    kubectl apply -f - <<EOF
apiVersion: pipelinesascode.tekton.dev/v1alpha1
kind: Repository
metadata:
  name: ${APPLICATION}-repository
  namespace: ${TENANT_NS}
spec:
  url: "${REPOSITORY_URL}"
EOF
fi

if [[ "${has_all_integration_values}" == "true" ]]; then
    echo "Applying IntegrationTestScenario resource..."
    kubectl apply -f - <<EOF
apiVersion: appstudio.redhat.com/v1beta2
kind: IntegrationTestScenario
metadata:
  name: ${COMPONENT}-integration
  namespace: ${TENANT_NS}
  labels:
    test.appstudio.openshift.io/optional: "false"
spec:
  application: ${APPLICATION}
  contexts:
    - name: application
      description: Application testing
  resolverRef:
    resolver: git
    params:
      - name: url
        value: "${INTEGRATION_GIT_URL}"
      - name: revision
        value: "${INTEGRATION_REVISION}"
      - name: pathInRepo
        value: "${INTEGRATION_PATH}"
EOF
fi

if [[ "${REGISTRY_MODE}" == "in-cluster" ]]; then
    BUILD_PIPELINE_SA="build-pipeline-${COMPONENT}"

    echo "Applying empty docker config secret '${REGISTRY_SECRET_NAME}' for in-cluster registry..."
    kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: ${REGISTRY_SECRET_NAME}
  namespace: ${TENANT_NS}
type: kubernetes.io/dockerconfigjson
stringData:
  # Keep a valid non-empty auth token so both select-oci-auth and cosign can parse it.
  .dockerconfigjson: '{"auths":{"registry-service.kind-registry":{"auth":"eDp4","email":""}}}'
EOF

    echo "Waiting for ServiceAccount '${BUILD_PIPELINE_SA}' to be created by Build Service..."
    wait_for_serviceaccount "${BUILD_PIPELINE_SA}"

    echo "Attaching secret '${REGISTRY_SECRET_NAME}' to ServiceAccount '${BUILD_PIPELINE_SA}'..."
    kubectl patch serviceaccount "${BUILD_PIPELINE_SA}" -n "${TENANT_NS}" \
      --type merge \
      -p "{\"secrets\":[{\"name\":\"${REGISTRY_SECRET_NAME}\"}]}" >/dev/null
fi

echo ""
echo "Component onboarding resources are configured."
