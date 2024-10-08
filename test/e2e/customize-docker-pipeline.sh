#!/bin/bash -e
set -x

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Updating docker-build pipeline to workaround issue with clair-scan" >&2
    # shellcheck disable=SC1091
    source "${script_path}/vars.sh"
    local original_docker_build_bundle_ref

    # This is required in order to push the modified pipeline to local registry
    kubectl port-forward -n kind-registry svc/registry-service 30001:443 &
    original_docker_build_bundle_ref=$(yq ".data[\"config.yaml\"]" konflux-ci/build-service/core/build-pipeline-config.yaml | yq ".pipelines[] | select(.name == \"docker-build\").bundle")
    # Remove the problematic "clair-scan" task from the docker pipeline
    tkn bundle list --remote-skip-tls "$original_docker_build_bundle_ref" -o yaml \
        | yq 'del(.spec.tasks[] | select(.name == "clair-scan"))' > "/tmp/customized-docker-pipeline.yaml"
    tkn bundle push --remote-skip-tls "$CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_LOCALHOST" \
        -f "/tmp/customized-docker-pipeline.yaml"
    # Update the bundle ref in build-service pipeline configmap
    sed -i "s|bundle:.*docker-build.*|bundle: ${CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_CLUSTER}|g" konflux-ci/build-service/core/build-pipeline-config.yaml
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
