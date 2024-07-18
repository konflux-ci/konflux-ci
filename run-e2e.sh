#!/bin/bash -e

E2E_TEST_IMAGE=quay.io/psturc/e2e-tests:KONFLUX-3683

main() {
    echo "Running E2E tests" >&2
    local github_org="${GH_ORG:?A GitHub org where the https://github.com/redhat-appstudio-qe/konflux-ci-sample repo is present/forked should be provided}"
    local github_token="${GH_TOKEN:?A GitHub token should be provided}"
    local quay_dockerconfig="${QUAY_DOCKERCONFIGJSON:?quay.io credentials in .dockerconfig format should be provided}"
    docker run --network=host -v ~/.kube/config:/kube/config --env KUBECONFIG=/kube/config -e GITHUB_TOKEN="$github_token" -e QUAY_TOKEN="$(base64 <<< "$quay_dockerconfig")" -e MY_GITHUB_ORG="$github_org" -e E2E_APPLICATIONS_NAMESPACE=user-ns2 $E2E_TEST_IMAGE "--ginkgo.label-filter=upstream-konflux" "--ginkgo.focus=Test local" "--ginkgo.v"
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
