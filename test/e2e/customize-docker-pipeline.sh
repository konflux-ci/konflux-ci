#!/bin/bash
set -euo pipefail

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

update_pipeline() {
    local pipeline_name=$1
    local tmp_pipeline_path="/tmp/customized-${pipeline_name}.yaml"
    local original_pipeline_bundle_ref

    original_pipeline_bundle_ref=$(yq ".data[\"config.yaml\"]" konflux-ci/build-service/core/build-pipeline-config.yaml | yq ".pipelines[] | select(.name == \"${pipeline_name}\").bundle")
    tkn bundle list --remote-skip-tls "$original_pipeline_bundle_ref" -o yaml > "${tmp_pipeline_path}"

    for task_name in $(yq "(.spec.tasks[] | .name)" "${tmp_pipeline_path}");
    do
        local original_task_bundle_ref
        local tmp_task_path="/tmp/customized-task.yaml"
        local customized_task_image_ref_localhost="localhost:30001/test/test:customized-task-${task_name}-${pipeline_name}"
        local customized_task_image_ref_cluster="registry-service.kind-registry/test/test:customized-task-${task_name}-${pipeline_name}"

        original_task_bundle_ref=$(yq "(.spec.tasks[] | select(.name == \"${task_name}\").taskRef.params[] | select(.name == \"bundle\").value)" "${tmp_pipeline_path}")
        tkn bundle list --remote-skip-tls "$original_task_bundle_ref" -o yaml > $tmp_task_path || continue

        yq e -i '(.spec.steps[] | select(has("computeResources")) | .computeResources.requests.cpu) |= "100m"' $tmp_task_path
        yq e -i '(.spec.steps[] | select(has("computeResources")) | .computeResources.requests.memory) |= "256Mi"' $tmp_task_path
        yq e -i '(.spec.steps[] | select(has("resources")) | .resources.requests.cpu) |= "100m"' $tmp_task_path
        yq e -i '(.spec.steps[] | select(has("resources")) | .resources.requests.memory) |= "256Mi"' $tmp_task_path

        yq e -i "(.spec.tasks[] | select(.name == \"${task_name}\").taskRef.params[] | select(.name == \"bundle\").value) |= \"${customized_task_image_ref_cluster}\"" "${tmp_pipeline_path}"

        tkn bundle push --remote-skip-tls "$customized_task_image_ref_localhost" -f $tmp_task_path
    done

    local customized_pipeline_image_ref_localhost="localhost:30001/test/test:customized-${pipeline_name}-pipeline"
    tkn bundle push --remote-skip-tls "$customized_pipeline_image_ref_localhost" -f "${tmp_pipeline_path}"

    # Update the bundle ref in build-service pipeline configmap
    local customized_pipeline_image_ref_cluster="registry-service.kind-registry/test/test:customized-${pipeline_name}-pipeline"
    sed -i "s|bundle:.*${pipeline_name}@.*|bundle: ${customized_pipeline_image_ref_cluster}|g" konflux-ci/build-service/core/build-pipeline-config.yaml
}

main() {
    echo "Updating docker-build pipeline to workaround the issues with and cpu/mem requests for build task" >&2
    # shellcheck disable=SC1091
    source "${script_path}/vars.sh"

    # This is required in order to push the modified pipeline to local registry
    kubectl port-forward -n kind-registry svc/registry-service 30001:443 &

    update_pipeline docker-build-oci-ta
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
