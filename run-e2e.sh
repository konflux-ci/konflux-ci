#!/bin/bash -e

E2E_TEST_IMAGE=quay.io/psturc/e2e-tests:KONFLUX-3683

main() {
    echo "Running E2E tests" >&2
    local github_org="${GH_ORG:?A GitHub org where the https://github.com/redhat-appstudio-qe/konflux-ci-sample repo is present/forked should be provided}"
    local github_token="${GH_TOKEN:?A GitHub token should be provided}"
    local quay_dockerconfig="${QUAY_DOCKERCONFIGJSON:?quay.io credentials in .dockerconfig format should be provided}"
    local ret=0
    docker run --network=host -v ~/.kube/config:/kube/config --env KUBECONFIG=/kube/config -e GITHUB_TOKEN="$github_token" -e QUAY_TOKEN="$(base64 <<< "$quay_dockerconfig")" -e MY_GITHUB_ORG="$github_org" -e E2E_APPLICATIONS_NAMESPACE=user-ns2 $E2E_TEST_IMAGE "--ginkgo.label-filter=upstream-konflux" "--ginkgo.focus=Test local" "--ginkgo.v" || ret="$?"
    if [ "$ret" != "0" ]; then
        set -x
        kubectl get nodes -o yaml
        kubectl run clair2 -n default --attach=true --restart=Never --image quay.io/redhat-appstudio/clair-in-ci:v1 --command -- "clair-action" "report" "--image-ref=quay.io/psturc_org/user-ns2/konflux-ci-upstream/konflux-ci-upstream@sha256:06ed74b1a0e4cce488ef5f831fcd403385bda21ad2ec27d034b8c1a54ed59fb2" "--db-path=/tmp/matcher.db" "--format=quay"
        kubectl get pr -A -o yaml
        kubectl get tr -A -o yaml
        for pod in $(kubectl get pods -n user-ns2 -o name); do echo "Logs for $pod:" && kubectl logs "$pod" -n user-ns2 --all-containers=true; done
        kubectl get events -n user-ns2
        kubectl get configmap -n build-service -o yaml
    fi
}


if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
