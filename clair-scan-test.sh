#!/bin/bash -e
set -x

main() {
    echo "Test CLAIR-SCAN TaskRun" >&2
    source ./deploy-deps.sh
    local ns=test-clair-scan
    kubectl create ns $ns
    kubectl create serviceaccount appstudio-pipeline -n $ns
    kubectl create -f https://gist.githubusercontent.com/psturc/0416344288f1dfc9a2b42556eab63a99/raw/dd4a22969830f9627988d688b161751f7a1df7a5/clair-scan-task.yaml -n $ns
    kubectl create -f https://gist.githubusercontent.com/psturc/0416344288f1dfc9a2b42556eab63a99/raw/97f0bdf9a71882223e18b93c4abc215f7956c497/clair-scan-taskrun.yaml -n $ns
    retry "kubectl get pod test-clair-scan-pod -n $ns"
    kubectl wait --timeout=120s --for=jsonpath='{.status.phase}'=Running pod/test-clair-scan-pod -n $ns
    kubectl logs -f test-clair-scan-pod --all-containers=true -n $ns
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi