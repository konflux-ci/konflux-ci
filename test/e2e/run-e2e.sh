#!/bin/bash -ex

script_path="$(dirname -- "${BASH_SOURCE[0]}")"

main() {
    echo "Running E2E tests" >&2
    # shellcheck disable=SC1091
    source "${script_path}/vars.sh"
    local github_org="${GH_ORG:?A GitHub org where the https://github.com/redhat-appstudio-qe/konflux-ci-sample repo is present/forked should be provided}"
    local github_token="${GH_TOKEN:?A GitHub token should be provided}"
    local quay_dockerconfig="${QUAY_DOCKERCONFIGJSON:?quay.io credentials in .dockerconfig format should be provided}"
    podman run --network=host -e ARTIFACT_DIR=/home -v ./:/home -v ~/.kube/config:/kube/config --env KUBECONFIG=/kube/config -e GITHUB_TOKEN="$github_token" -e QUAY_TOKEN="$(base64 <<< "$quay_dockerconfig")" -e MY_GITHUB_ORG="$github_org" -e E2E_APPLICATIONS_NAMESPACE=user-ns2 "$E2E_TEST_IMAGE" /konflux-e2e/konflux-e2e.test "--ginkgo.label-filter=upstream-konflux" "--ginkgo.json-report=/home/test.jso" "--ginkgo.focus=Test local" "--ginkgo.v"
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
