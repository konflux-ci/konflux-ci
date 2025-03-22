#!/bin/bash -e
set -x

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

update_pipeline() {
    local pipeline_name=$1
    local customized_build_task_image_ref_localhost="localhost:30001/test/test:customized-build-task-${pipeline_name}"
    local customized_build_task_image_ref_cluster="registry-service.kind-registry/test/test:customized-build-task-${pipeline_name}"
    local customized_cert_checks_task_image_ref_localhost="localhost:30001/test/test:customized-cert-checks-task-${pipeline_name}"
    local customized_cert_checks_task_image_ref_cluster="registry-service.kind-registry/test/test:customized-cert-checks-task-${pipeline_name}"
    local customized_pipeline_image_ref_localhost="localhost:30001/test/test:customized-${pipeline_name}-pipeline"
    local customized_pipeline_image_ref_cluster="registry-service.kind-registry/test/test:customized-${pipeline_name}-pipeline"

    local original_pipeline_bundle_ref original_build_task_bundle_ref original_cert_checks_task_bundle_ref
    local tmp_pipeline_path="/tmp/customized-docker-pipeline.yaml"
    local tmp_build_task_path="/tmp/customized-build-container-task.yaml"
    local tmp_cert_checks_task_path="/tmp/customized-cert-checks-task.yaml"
    # Pull the required Pipeline and Task bundles
    original_pipeline_bundle_ref=$(yq ".data[\"config.yaml\"]" konflux-ci/build-service/core/build-pipeline-config.yaml | yq ".pipelines[] | select(.name == \"${pipeline_name}\").bundle")
    tkn bundle list --remote-skip-tls "$original_pipeline_bundle_ref" -o yaml > $tmp_pipeline_path
    original_build_task_bundle_ref=$(yq "(.spec.tasks[] | select(.name == \"build-container\").taskRef.params[] | select(.name == \"bundle\").value)" $tmp_pipeline_path)
    tkn bundle list --remote-skip-tls "$original_build_task_bundle_ref" -o yaml > $tmp_build_task_path
    original_cert_checks_task_bundle_ref=$(yq "(.spec.tasks[] | select(.name == \"ecosystem-cert-preflight-checks\").taskRef.params[] | select(.name == \"bundle\").value)" $tmp_pipeline_path)
    tkn bundle list --remote-skip-tls "$original_cert_checks_task_bundle_ref" -o yaml > $tmp_cert_checks_task_path
    # Workaround
    # CPU and memory requests are too high for the build-container Task's "build" step to run in GitHub actions
    # Thus this workaround lowers ".computeResources.requests" from the "build" step and updates the build Pipeline that is used in CI to reference the updated Task
    yq e -i "(.spec.steps[].computeResources.requests.cpu) |= \"100m\"" $tmp_build_task_path
    yq e -i "(.spec.steps[].computeResources.requests.memory) |= \"256Mi\"" $tmp_build_task_path
    yq e -i "(.spec.steps[].resources.requests.cpu) |= \"100m\"" $tmp_cert_checks_task_path
    yq e -i "(.spec.steps[].resources.requests.memory) |= \"256Mi\"" $tmp_cert_checks_task_path
    yq e -i "(.spec.tasks[] | select(.name == \"build-container\").taskRef.params[] | select(.name == \"bundle\").value) |= \"${customized_build_task_image_ref_cluster}\"" $tmp_pipeline_path
    yq e -i "(.spec.tasks[] | select(.name == \"ecosystem-cert-preflight-checks\").taskRef.params[] | select(.name == \"bundle\").value) |= \"${customized_cert_checks_task_image_ref_cluster}\"" $tmp_pipeline_path

    # Push the modified Pipeline and Task bundles to the local container registry
    tkn bundle push --remote-skip-tls "$customized_pipeline_image_ref_localhost" \
        -f $tmp_pipeline_path
    tkn bundle push --remote-skip-tls "$customized_build_task_image_ref_localhost" \
        -f $tmp_build_task_path
    tkn bundle push --remote-skip-tls "$customized_cert_checks_task_image_ref_localhost" \
        -f $tmp_cert_checks_task_path
    # Update the bundle ref in build-service pipeline configmap
    sed -i "s|bundle:.*${pipeline_name}@.*|bundle: ${customized_pipeline_image_ref_cluster}|g" konflux-ci/build-service/core/build-pipeline-config.yaml
}

main() {
    echo "Updating docker-build pipeline to workaround the issues with and cpu/mem requests for build task" >&2
    # shellcheck disable=SC1091
    source "${script_path}/vars.sh"

    # This is required in order to push the modified pipeline to local registry
    kubectl port-forward -n kind-registry svc/registry-service 30001:443 &

    update_pipeline docker-build
    update_pipeline docker-build-oci-ta
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
